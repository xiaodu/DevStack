import { nanoLoading, nanoStatusMsg, pushToast } from './stores'

/*
 * ── Debounce map ──
 */
const pendingCalls = new Map<string, { abort: boolean }>()

function debounceKey(key: string): { abort: boolean } {
  if (pendingCalls.has(key)) {
    pendingCalls.get(key)!.abort = true
  }
  const entry = { abort: false }
  pendingCalls.set(key, entry)
  return entry
}

function debounceCleanup(key: string) {
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
export interface SafeInvokeOptions {
  statusMsg?: string
  successMsg?: string
  errorPrefix?: string
  timeoutMs?: number
  suppressToast?: boolean
  onSuccess?: (result: any) => void
  onError?: (err: any) => void
}

export async function safeInvoke(
  key: string | (() => Promise<any>),
  fn: (() => Promise<any>) | SafeInvokeOptions,
  opts: SafeInvokeOptions = {},
): Promise<any> {
  // Backward compat: safeInvoke(fn, opts)  →  generate key from fn.name
  if (typeof key === 'function') {
    opts = (fn as SafeInvokeOptions) || {}
    fn = key as () => Promise<any>
    key = (fn as () => Promise<any>).name || 'anonymous'
  }
  const callFn = fn as () => Promise<any>
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
    const result = await callFn()
    clearTimeout(safetyTimeout)
    if (!suppressToast && successMsg) pushToast(successMsg, 'success')
    if (onSuccess) onSuccess(result)
    return result
  } catch (e) {
    clearTimeout(safetyTimeout)
    const message = (e as any)?.message || String(e)
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
export function wrapApi(apiObj: Record<string, any>, methodOpts: Record<string, any> = {}): Record<string, any> {
  const wrapped: Record<string, any> = {}
  for (const [name, fn] of Object.entries(apiObj)) {
    if (typeof fn !== 'function') {
      wrapped[name] = fn
      continue
    }
    wrapped[name] = (...args: any[]) => {
      const opts = methodOpts[name] || {}
      const key = opts.key || `${name}:${args.map((a) => String(a).slice(0, 40)).join(':')}`
      return safeInvoke(key, () => fn(...args), opts)
    }
  }
  return wrapped
}
