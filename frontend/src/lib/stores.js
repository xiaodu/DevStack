import { writable } from 'svelte/store'

export const services = writable([])
export const sites = writable([])
export const settings = writable({})
export const systemInfo = writable({})
export const loading = writable(false)
export const error = writable(null)
