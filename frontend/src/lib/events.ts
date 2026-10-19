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

/*
 * ── 应用层事件分发（引用计数）──
 *
 * ⚠️ Wails v2 的 EventsOff 签名坑（见 runtime/desktop/events.js）：
 *   EventsOff(eventName, ...additionalEventNames)
 * 第二个参数是「另一个事件名」，永远不会被当成回调，也不会抛异常；它内部对每个名字
 * 调 removeListener(name)，一次性删掉该事件名下的全部监听（还会顺带通知 Go 侧解绑）。
 * 所以旧写法 rt.EventsOff(name, cb) 的 try/catch 兜底是死代码，实际效果是「按名全删」，
 * 会静默杀掉其它组件的同名监听（例如 Environment 卸载会带走 app 级 download:phase 刷新）。
 *
 * Wails 对外契约里也没有可用的按回调退订：生成的 runtime.d.ts 声明
 * EventsOn(...): void、EventsOff(eventName): void。因此精确退订在我们自己这一层做：
 * 每个事件名只向 Wails 注册一个多路分发器，监听者集合做引用计数，
 * 计数归零才对 Wails 调 EventsOff(name)（不带第二个参数）。
 */
type EventHandler = (...args: any[]) => void
// 每次订阅一个独立记录（而非直接用函数做 key），保证同一函数多次订阅也能各自精确退订
interface Subscription {
  handler: EventHandler
}

const handlerSets = new Map<string, Set<Subscription>>()
// 已注册到 Wails 的事件名（每个名字只有一个分发器）
const attached = new Set<string>()

function attachDispatcher(rt: any, eventName: string) {
  if (attached.has(eventName)) return
  attached.add(eventName)
  rt.EventsOn(eventName, (...args: any[]) => {
    const set = handlerSets.get(eventName)
    if (!set || set.size === 0) return
    // 拷贝一份：处理过程中若有回调退订/订阅，不影响本次派发
    for (const sub of [...set]) {
      try {
        sub.handler(...args)
      } catch (e) {
        console.error(`[events] "${eventName}" 回调抛错：`, e)
      }
    }
  })
}

function detachDispatcher(eventName: string) {
  if (!attached.has(eventName)) return
  attached.delete(eventName)
  const rt = getWailsRuntime()
  // 唯一正确用法：只传事件名，此时该事件名下只有我们自己的分发器，删除是安全的
  if (rt && typeof rt.EventsOff === 'function') rt.EventsOff(eventName)
}

// 返回精确退订函数：仅移除本次注册的回调，不影响同名其他监听（F5）
function EventsOn(eventName: string, callback: EventHandler) {
  const rt = getWailsRuntime()
  if (!rt || typeof rt.EventsOn !== 'function') return () => {}

  let set = handlerSets.get(eventName)
  if (!set) {
    set = new Set()
    handlerSets.set(eventName, set)
  }
  const sub: Subscription = { handler: callback }
  set.add(sub)
  // 该事件名的监听者从 0 变 1，才向 Wails 注册分发器
  if (set.size === 1) attachDispatcher(rt, eventName)

  let removed = false
  return () => {
    if (removed) return
    removed = true
    const cur = handlerSets.get(eventName)
    if (!cur) return
    cur.delete(sub)
    if (cur.size === 0) {
      handlerSets.delete(eventName)
      detachDispatcher(eventName)
    }
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
