import { writable } from 'svelte/store'

export const services = writable([])
export const sites = writable([])
export const settings = writable({})
export const systemInfo = writable({})
export const loading = writable(false)
export const error = writable(null)
export const refreshSignal = writable(0)
export const toasts = writable([])
export const globalLoading = writable(false)

export function addToast(msg, type = 'success', duration = 3000) {
  const id = Date.now() + Math.random()
  toasts.update(t => [...t, { id, msg, type }])
  setTimeout(() => toasts.update(t => t.filter(x => x.id !== id)), duration)
}
