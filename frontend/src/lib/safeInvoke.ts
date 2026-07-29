import { nanoLoading, nanoStatusMsg, pushToast } from './stores'

/*
 * ── Debounce map ──
 */
const pendingCalls = new Map()

function debounceKey(key) {
  if (pendingCalls.has(key)) {
    pendingCalls.get(key).abort = true
  }
  const entry = { abort: false }
  pendingCalls.set(key, entry)
  return entry
}

function debounceCleanup(key) {
  pendingCalls.delete(key)
}

/*
 * ── safeInvoke ──
 *
 * Wraps any async Wails IPC call with:
 *  - Automatic global loading state
 *  - Status message during execution
 *  - Success/error toast feedback
 *  - Debounce by key (prevents duplicate concurrent calls)
 *  - Safety timeout (force-clears loading after N seconds)
 *  - Error boundary (never throws uncaught)
 *
 * @param {string}   key          — Unique call identity for debounce (e.g. 'startService:nginx')
 * @param {Function} fn           — Async function that returns a promise
 * @param {Object}   opts
 * @param {string}   [opts.statusMsg]   — Status bar text during execution
 * @param {string}   [opts.successMsg]  — Toast message on success
 * @param {string}   [opts.errorPrefix] — Prefix for error toast
 * @param {number}   [opto.timeoutMs]   — Safety timeout in ms (default 15000)
 * @param {boolean}  [opts.suppressToast] — Suppress all toasts
 * @param {Function} [opts.onSuccess]    — Extra callback on success
 * @param {Function} [opts.onError]      — Extra callback on error
 * @returns {Promise<any>}
 */
export async function safeInvoke(key, fn, opts = {}) {
  // Backward compat: safeInvoke(fn, opts)  →  generate key from fn.name
  if (typeof key === 'function') {
    opts = fn || {}
    fn = key
    key = fn.name || 'anonymous'
  }
  const { statusMsg, successMsg, errorPrefix, timeoutMs = 15000, suppressToast = false, onSuccess, onError } = opts

  // ── Debounce: abort if another call with same key is in-flight ──
  const guard = debounceKey(key)
  if (guard.abort) {
    console.warn(`[safeInvoke] debounced: "${key}" already pending`)
    return
  }

  // ── Enter loading ──
  nanoLoading.set(true)
  if (statusMsg) nanoStatusMsg.set(statusMsg)

  const safetyTimeout = setTimeout(() => {
    console.warn(`[safeInvoke] safety timeout for "${key}" after ${timeoutMs}ms`)
    nanoLoading.set(false)
    nanoStatusMsg.set('')
    debounceCleanup(key)
  }, timeoutMs)

  try {
    const result = await fn()
    clearTimeout(safetyTimeout)
    if (!suppressToast && successMsg) pushToast(successMsg, 'success')
    if (onSuccess) onSuccess(result)
    return result
  } catch (e) {
    clearTimeout(safetyTimeout)
    const message = e?.message || String(e)
    console.error(`[safeInvoke] "${key}" failed:`, message)
    if (!suppressToast) {
      pushToast(errorPrefix ? `${errorPrefix}: ${message}` : message, 'error')
    }
    if (onError) onError(e)
    throw e
  } finally {
    nanoLoading.set(false)
    nanoStatusMsg.set('')
    debounceCleanup(key)
  }
}

/*
 * ── Convenience: wrap an entire api object ──
 *
 * const api2 = wrapApi(api, {
 *   getServices: { successMsg: '…', statusMsg: '…' },
 * })
 *
 * Each method gets a default key based on its name + first arg.
 */
export function wrapApi(apiObj, methodOpts = {}) {
  const wrapped = {}
  for (const [name, fn] of Object.entries(apiObj)) {
    if (typeof fn !== 'function') {
      wrapped[name] = fn
      continue
    }
    wrapped[name] = (...args) => {
      const opts = methodOpts[name] || {}
      const key = opts.key || `${name}:${args.map((a) => String(a).slice(0, 40)).join(':')}`
      return safeInvoke(key, () => fn(...args), opts)
    }
  }
  return wrapped
}
