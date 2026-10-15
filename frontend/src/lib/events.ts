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

// 返回精确退订函数：仅移除本次注册的回调，不影响同名其他监听（F5）
function EventsOn(eventName: string, callback: (...args: any[]) => void) {
  const rt = getWailsRuntime()
  if (rt && typeof rt.EventsOn === 'function') {
    rt.EventsOn(eventName, callback)
    return () => EventsOff(eventName, callback)
  }
  return () => {}
}

function EventsOff(eventName: string, cb?: (...args: any[]) => void) {
  const rt = getWailsRuntime()
  if (rt && typeof rt.EventsOff === 'function') {
    if (cb) {
      try {
        rt.EventsOff(eventName, cb)
        return
      } catch {
        // 部分 Wails 版本的 EventsOff 不接受回调参数，回退到按名全删
      }
    }
    rt.EventsOff(eventName)
  }
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
export function onWailsEvent(eventName: string, handler: (...args: any[]) => void) {
  return EventsOn(eventName, handler)
}

/*
 * ── Subscribe in a component's script (auto-cleanup via onDestroy) ──
 * Usage:
 *
 *   useWailsEvent('reactive:service.started', (data) => updateUI(data))
 */
export function useWailsEvent(eventName: string, handler: (...args: any[]) => void) {
  const unsub = EventsOn(eventName, handler)
  onDestroy(unsub)
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
    'reactive:service.starting',
    'reactive:service.stopping',
    'reactive:service.crashed',
    'reactive:service.restarted',
    'reactive:service.health_failed',
    'reactive:service.error',
    'reactive:port.conflict',
    'reactive:config.changed',
    'reactive:config.reloaded',
    'reactive:resource.allocated',
    'reactive:resource.released',
    'reactive:project.created',
    'reactive:project.deleted',
    'reactive:project.enabled',
    'reactive:project.disabled',
    'reactive:overflow', // 后端事件丢弃兜底：合并触发一次全量刷新
  ]

  const unsubs = events.map((name) =>
    EventsOn(name, (data) => {
      console.debug('[events]', name, data)
      nanoRefresh.set(nanoRefresh.get() + 1)
    }),
  )

  // 安装/卸载完成（download:phase 到达 done/error）也要刷新已安装状态与按钮，
  // 否则 Environment/Packages 页要切走再进来才更新（#1）。
  unsubs.push(
    EventsOn('download:phase', (data: any) => {
      if (data?.phase === 'done' || data?.phase === 'error') {
        nanoRefresh.set(nanoRefresh.get() + 1)
      }
    }),
  )

  return () => unsubs.forEach((fn) => fn())
}

/*
 * ── Listen to download progress ──
 *
 *   useDownloadProgress(({ key, service, version, downloaded, total }) => ...)
 */
export function useDownloadProgress(handler: (data: any) => void) {
  const unsub = EventsOn('download:progress', handler)
  onDestroy(unsub)
}

/*
 * ── Listen to download phase changes ──
 *
 *   useDownloadPhase(({ service, version, phase, downloaded, total }) => ...)
 *   phase: 'downloading' | 'extracting' | 'configuring' | 'done' | 'error'
 */
export function useDownloadPhase(handler: (data: any) => void) {
  const unsub = EventsOn('download:phase', handler)
  onDestroy(unsub)
}

/*
 * ── 环境编排进度事件（operation:begin / operation:step / operation:end） ──
 *
 * handler 收到 (eventName, data)：
 *   'begin' → { action, title, steps: [{id,label,state,message}...] }
 *   'step'  → { id, state: 'active'|'done'|'error', message }
 *   'end'   → { action, ok, message }
 */
export function subscribeOperationEvents(handler: (eventName: string, data: any) => void) {
  const names = ['operation:begin', 'operation:step', 'operation:end']
  const unsubs = names.map((name) =>
    EventsOn(name, (data: any) => {
      handler(name.replace('operation:', ''), data)
    }),
  )
  return () => unsubs.forEach((fn) => fn())
}
