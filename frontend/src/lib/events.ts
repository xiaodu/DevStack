import { onDestroy } from 'svelte'
import { nanoRefresh } from './stores'

// Wails runtime 在浏览器环境下不存在，通过 window.runtime 动态访问
// wails 应用启动时会注入 window.runtime 对象
function getWailsRuntime() {
  if (typeof window !== 'undefined' && (window as any).runtime) {
    return (window as any).runtime
  }
  return null
}

function EventsOn(eventName: string, callback: (...args: any[]) => void) {
  const rt = getWailsRuntime()
  if (rt && typeof rt.EventsOn === 'function') {
    return rt.EventsOn(eventName, callback)
  }
  return () => {}
}

function EventsOff(eventName: string) {
  const rt = getWailsRuntime()
  if (rt && typeof rt.EventsOff === 'function') {
    return rt.EventsOff(eventName)
  }
  return () => {}
}

/*
 * ── Subscribe to a Wails event with auto-cleanup ──
 * Use inside `onMount`:
 *
 *   import { onWailsEvent } from '../lib/events'
 *
 *   onMount(() => {
 *     const unsub = onWailsEvent('reactive:service.started', (data) => { ... })
 *     return unsub
 *   })
 */
export function onWailsEvent(eventName, handler) {
  EventsOn(eventName, handler)
  const unsub = () => EventsOff(eventName)
  return unsub
}

/*
 * ── Subscribe in a component's script (auto-cleanup via onDestroy) ──
 * Usage:
 *
 *   useWailsEvent('reactive:service.started', (data) => updateUI(data))
 */
export function useWailsEvent(eventName, handler) {
  EventsOn(eventName, handler)
  onDestroy(() => EventsOff(eventName))
}

/*
 * ── Convenience: listen to all reactive events and refresh stores ──
 * Call once in App.svelte:
 *
 *   onMount(() => listenReactiveEvents())
 */
export function subscribeReactiveEvents() {
  const events = [
    'reactive:service.started',
    'reactive:service.stopped',
    'reactive:service.crashed',
    'reactive:service.restarted',
    'reactive:service.health_failed',
    'reactive:port.conflict',
    'reactive:config.changed',
    'reactive:config.reloaded',
    'reactive:resource.allocated',
    'reactive:resource.released',
    'reactive:project.created',
    'reactive:project.deleted',
    'reactive:project.enabled',
    'reactive:project.disabled',
  ]

  const unsubs = events.map((name) => {
    EventsOn(name, (data) => {
      console.debug('[events]', name, data)
      nanoRefresh.set(nanoRefresh.get() + 1)
    })
    return () => EventsOff(name)
  })

  return () => unsubs.forEach((fn) => fn())
}

/*
 * ── Listen to download progress ──
 *
 *   useDownloadProgress(({ key, service, version, downloaded, total }) => ...)
 */
export function useDownloadProgress(handler) {
  EventsOn('download:progress', handler)
  onDestroy(() => EventsOff('download:progress'))
}
