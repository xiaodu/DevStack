import { atom, computed, type WritableAtom } from 'nanostores'
import { writable } from 'svelte/store'
import { api } from './api'

/*
 * ── nanostores atoms (framework-agnostic) ──
 */
export interface Toast {
  id: number
  msg: string
  type: string
}

// 全局遮罩用引用计数而非布尔：并发多个 IPC 时，先结束的那个不应提前关掉遮罩（F2）。
// 仅 beginLoading/endLoading 会改它，值 >0 即显示。
export const nanoLoading = atom(0)
export const nanoStatusMsg = atom('')
export const nanoToasts = atom<Toast[]>([])
export const nanoRefresh = atom(0)
export const nanoServices = atom<any[]>([])
export const nanoRuntimes = atom<any[]>([])
export const nanoSites = atom<any[]>([])
export const nanoSettings = atom<any>({})
export const nanoSysInfo = atom<any>({})
export const nanoLastError = atom<any>(null)

// beginLoading/endLoading 成对使用；endLoading 由调用方保证每次 begin 恰好调用一次。
export function beginLoading() {
  nanoLoading.set(nanoLoading.get() + 1)
}
export function endLoading() {
  const next = nanoLoading.get() - 1
  nanoLoading.set(next > 0 ? next : 0)
}

// 后台同步状态（用于状态栏展示，区别于全屏 loading）
export interface SyncInfo {
  syncing: boolean
  msg: string
  at: string
}
export const nanoSyncInfo = atom<SyncInfo>({ syncing: false, msg: '', at: '' })

export const nanoIsBusy = computed(nanoLoading, (v) => v > 0)

/*
 * ── 环境编排操作状态 ──
 *
 * 一次"动作"（激活/运行/停止环境）从开始到结束的完整编排状态。
 * 全屏进度由 GlobalOperation.svelte 消费；按钮禁用由 opActive 控制。
 */
export interface OpStep {
  id: string
  label: string
  state: 'pending' | 'active' | 'done' | 'error'
  message: string
}

export interface Operation {
  active: boolean
  status: 'idle' | 'running' | 'success' | 'error'
  action: string
  title: string
  steps: OpStep[]
  message: string
  error: string
  startedAt: number
}

export const IDLE_OPERATION: Operation = {
  active: false,
  status: 'idle',
  action: '',
  title: '',
  steps: [],
  message: '',
  error: '',
  startedAt: 0,
}

export const nanoOperation = atom<Operation>(IDLE_OPERATION)

/*
 * ── Toast helpers ──
 */
let toastId = 0

export function pushToast(msg: string, type = 'success', duration = 3500): number {
  const id = ++toastId
  nanoToasts.set([...nanoToasts.get(), { id, msg, type }])
  if (duration > 0) setTimeout(() => dismissToast(id), duration)
  // 将 toast 同步写入后端日志，保证所有可见消息可在日志页查询
  api.logToast(String(msg), String(type)).catch(() => {})
  return id
}

export function dismissToast(id: number) {
  nanoToasts.set(nanoToasts.get().filter((t) => t.id !== id))
}

/*
 * ── Bidirectional bridge: nanostores ↔ Svelte stores ──
 * Writing to the writable also updates the atom, so Dashboard.load()
 * can call globalLoading.set() and it propagates both ways.
 */
function bridge<T>(atom: WritableAtom<T>) {
  const store = writable<T>(atom.get(), (set) => atom.subscribe((v) => set(v)))
  const origSet = store.set
  store.set = (v: T) => {
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
export const operation = bridge(nanoOperation)
export const syncInfo = bridge(nanoSyncInfo)

/*
 * ── Deprecated aliases ──
 */
export const loading = globalLoading
export const error = writable(null)
export function addToast(msg: string, type: string, duration: number): number {
  return pushToast(msg, type, duration)
}

/*
 * ── runOperation ──
 *
 * 统一入口：所有环境级长操作（激活/运行/停止）都经由这里，保证：
 *  - 全局去重：已有操作进行中则直接拒绝并提示，防止重复点击
 *  - 全屏进度：标题 + 步骤清单由后端 operation:begin/step 事件驱动
 *  - Toast 策略：进行中不弹；成功→overlay 收起后再弹成功；
 *    失败→overlay 内展示错误，自动收起后弹错误
 */
export interface RunOperationOptions {
  action: string
  title: string
  call: () => Promise<any>
  successMsg: string
  onDone?: () => void | Promise<void>
  /** 整体超时（毫秒），后端挂起时不再让遮罩永久锁死。默认 60s。 */
  timeoutMs?: number
}

let opTimer = 0
let opTimeout = 0

function scheduleOperationClose(status: 'success' | 'error', toast: string, delay: number) {
  clearTimeout(opTimer)
  opTimer = window.setTimeout(() => {
    nanoOperation.set({ ...nanoOperation.get(), active: false, status: 'idle' })
    pushToast(toast, status)
  }, delay)
}

/** 手动关闭操作遮罩（如超时后人工接管）。 */
export function dismissOperation() {
  clearTimeout(opTimer)
  clearTimeout(opTimeout)
  nanoOperation.set(IDLE_OPERATION)
}

export async function runOperation(opts: RunOperationOptions): Promise<boolean> {
  const cur = nanoOperation.get()
  if (cur.active) {
    pushToast('上一个操作正在进行，请稍候', 'warning')
    return false
  }
  clearTimeout(opTimer)
  clearTimeout(opTimeout)
  nanoOperation.set({
    ...IDLE_OPERATION,
    active: true,
    status: 'running',
    action: opts.action,
    title: opts.title,
    message: '正在准备...',
    startedAt: Date.now(),
  })

  // 整体看门狗：后端挂起时强制结束遮罩，避免永久锁死（F2）
  const timeoutMs = opts.timeoutMs || 60000
  opTimeout = window.setTimeout(() => {
    nanoOperation.set({
      ...nanoOperation.get(),
      status: 'error',
      error: '操作超时，请检查后端状态',
      message: '操作超时',
    })
    scheduleOperationClose('error', '操作超时，请检查后端状态', 4500)
  }, timeoutMs)

  try {
    await opts.call()
    if (opts.onDone) await opts.onDone()
    // operation:end(ok=false) 可能在 call 返回前先到，复查状态，
    // 失败的 op 不得被成功路径覆盖（F6）
    const after = nanoOperation.get()
    if (after.status === 'error') {
      scheduleOperationClose('error', `操作失败：${after.error}`, 4500)
      return false
    }
    nanoOperation.set({ ...after, status: 'success', message: '操作完成，正在同步状态…' })
    scheduleOperationClose('success', opts.successMsg, 1200)
    return true
  } catch (e) {
    const msg = (e as any)?.message || String(e)
    nanoOperation.set({ ...nanoOperation.get(), status: 'error', error: msg, message: '操作失败' })
    scheduleOperationClose('error', `操作失败：${msg}`, 4500)
    return false
  } finally {
    clearTimeout(opTimeout)
  }
}

/** 是否有编排操作正在执行（用于禁用相关按钮）。 */
export function isOpActive(): boolean {
  const o = nanoOperation.get()
  return o.active && o.status === 'running'
}
