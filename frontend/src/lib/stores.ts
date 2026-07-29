import { atom, computed } from 'nanostores'
import { writable } from 'svelte/store'

/*
 * ── nanostores atoms (framework-agnostic) ──
 */
export const nanoLoading = atom(false)
export const nanoStatusMsg = atom('')
export const nanoToasts = atom([])
export const nanoRefresh = atom(0)
export const nanoServices = atom([])
export const nanoRuntimes = atom([])
export const nanoSites = atom([])
export const nanoSettings = atom({})
export const nanoSysInfo = atom({})
export const nanoLastError = atom(null)

export const nanoIsBusy = computed(nanoLoading, (v) => v)

/*
 * ── Toast helpers ──
 */
let toastId = 0

export function pushToast(msg, type = 'success', duration = 3500) {
  const id = ++toastId
  nanoToasts.set([...nanoToasts.get(), { id, msg, type }])
  if (duration > 0) setTimeout(() => dismissToast(id), duration)
  return id
}

export function dismissToast(id) {
  nanoToasts.set(nanoToasts.get().filter((t) => t.id !== id))
}

/*
 * ── Bidirectional bridge: nanostores ↔ Svelte stores ──
 * Writing to the writable also updates the atom, so Dashboard.load()
 * can call globalLoading.set() and it propagates both ways.
 */
function bridge(atom) {
  const store = writable(atom.get(), (set) => atom.subscribe((v) => set(v)))
  const origSet = store.set.bind(store)
  store.set = (v) => {
    atom.set(v)
    origSet(v)
  }
  return store
}

export const globalLoading = bridge(nanoLoading)
export const statusMessage = bridge(nanoStatusMsg)
export const toasts = bridge(nanoToasts)
export const refreshSignal = bridge(nanoRefresh)
export const services = bridge(nanoServices)
export const runtimes = bridge(nanoRuntimes)
export const sites = bridge(nanoSites)
export const settings = bridge(nanoSettings)
export const systemInfo = bridge(nanoSysInfo)

/*
 * ── Deprecated aliases ──
 */
export const loading = globalLoading
export const error = writable(null)
export function addToast(msg, type, duration) {
  return pushToast(msg, type, duration)
}
