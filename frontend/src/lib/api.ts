import {
  ActivateEnvironment,
  ActivateEnvironmentForProject,
  ActivateEnvironmentOnly,
  ApplyServiceConfig,
  CancelDownload,
  CheckMySQLRootAuth,
  CheckSetupPorts,
  CheckVersionInstalled,
  CheckInstallDirChange,
  ClearLogs,
  CompleteSetup,
  CreateEnvironment,
  CreateProject,
  DeactivateEnvironment,
  DebugLog,
  DeleteEnvironment,
  DeleteProject,
  DeleteSnapshot,
  DownloadService,
  GetAvailableVersions,
  GetConfigPath,
  GetEnvironment,
  GetEnvironmentState,
  GetInstalledVersions,
  GetLogFiles,
  GetLogs,
  GetPHPExtensions,
  GetPHPRegistry,
  GetProjects,
  GetRecentLogLines,
  GetRecentOps,
  GetRuntimes,
  GetServiceConfig,
  GetServices,
  GetSettings,
  GetSnapshotDetail,
  GetSnapshots,
  GetSystemInfo,
  ListEnvironmentProfiles,
  ListEnvironments,
  LogToast,
  MigrateInstallDir,
  OpenDirectory,
  OpenFolder,
  ReadLogFile,
  RefreshPHPRegistry,
  RestartService,
  RollbackSnapshot,
  RunEnvironment,
  SavePHPExtensions,
  SaveProjectAsEnvironment,
  SaveServiceConfig,
  SaveSettings,
  SelectInstallDir,
  SetServiceVersion,
  StartRuntime,
  StartService,
  StopRuntime,
  StopService,
  TakeSnapshot,
  ToggleProject,
  UninstallService,
  UninstallVersion,
  UpdateEnvironment,
  UpdateProject,
} from '../../wailsjs/go/main/App.js'
import { onWailsEvent } from './events'

// 检测是否运行在 Wails 环境中（浏览器预览模式时 window.go 不存在）
const isWailsEnv = typeof window !== 'undefined' && (window as any).go !== undefined

// ====== Mock 数据（仅用于浏览器预览 UI 样式） ======
const mockEnvironments = [
  {
    id: 'builtin-lnmp-php74',
    name: 'LNMP (PHP 7.4 + MySQL 5.7 + Nginx 1.20)',
    description: '经典 LNMP 组合，兼容绝大多数国内 PHP 开源系统',
    isBuiltin: true,
    tags: ['LAMP', 'PHP', '推荐', '建站'],
    units: [
      { kind: 'nginx', version: '1.20.2', name: 'Nginx' },
      { kind: 'mysql', version: '5.7.44', name: 'MySQL' },
      { kind: 'php', version: '7.4.33', name: 'PHP' },
      { kind: 'redis', version: '7.2.5', name: 'Redis' },
    ],
    createdAt: '2024-01-01',
    updatedAt: '2024-01-01',
  },
  {
    id: 'builtin-modern-php8',
    name: '现代 PHP 全栈 (PHP 8.3 + MySQL 8.0 + Redis 7 + Nginx 1.24)',
    description: '面向 Laravel / Symfony / ThinkPHP 8 等现代框架的高性能组合',
    isBuiltin: true,
    tags: ['PHP8', 'Laravel', '高性能'],
    units: [
      { kind: 'nginx', version: '1.24.0', name: 'Nginx' },
      { kind: 'mysql', version: '8.0.39', name: 'MySQL' },
      { kind: 'php', version: '8.3.12', name: 'PHP' },
      { kind: 'redis', version: '7.2.5', name: 'Redis' },
    ],
    createdAt: '2024-01-01',
    updatedAt: '2024-01-01',
  },
  {
    id: 'builtin-node-web',
    name: 'Node 全栈 (Node 20 + Nginx 1.24 + Redis 7)',
    description: '适用于 Vue/Next/Nuxt 前端 SSR、Koa/Nest/Egg 服务端项目',
    isBuiltin: true,
    tags: ['Node', 'SSR', 'Vue', 'React'],
    units: [
      { kind: 'nginx', version: '1.24.0', name: 'Nginx' },
      { kind: 'redis', version: '7.2.5', name: 'Redis' },
      { kind: 'node', version: '20.18.0', name: 'Node.js' },
    ],
    createdAt: '2024-01-01',
    updatedAt: '2024-01-01',
  },
]

const mockServices = [
  {
    id: 'nginx@1.24.0',
    kind: 'nginx',
    name: 'Nginx',
    type: 'webserver',
    category: 'web',
    version: '1.24.0',
    status: 'running',
    port: 80,
    pid: 1234,
    installed: true,
  },
  {
    id: 'mysql@8.0.36',
    kind: 'mysql',
    name: 'MySQL',
    type: 'database',
    category: 'db',
    version: '8.0.36',
    status: 'running',
    port: 3306,
    pid: 5678,
    installed: true,
  },
  {
    id: 'redis@7.2.4',
    kind: 'redis',
    name: 'Redis',
    type: 'cache',
    category: 'cache',
    version: '7.2.4',
    status: 'stopped',
    port: 6379,
    pid: 0,
    installed: false,
  },
]

const mockRuntimes = [
  {
    id: 'php@8.3.0',
    kind: 'php',
    type: 'php',
    version: '8.3.0',
    path: '/usr/local/php8.3',
    active: true,
    running: true,
    port: 9000,
    installed: true,
    status: 'running',
    name: 'PHP 8.3.0',
    pid: 9012,
  },
  {
    id: 'php@7.4.33',
    kind: 'php',
    type: 'php',
    version: '7.4.33',
    path: '/usr/local/php7.4',
    active: false,
    running: false,
    port: 9001,
    installed: true,
    status: 'stopped',
    name: 'PHP 7.4.33',
    pid: 0,
  },
  {
    id: 'node@20.11.1',
    kind: 'node',
    type: 'node',
    version: '20.11.1',
    path: '/usr/local/node20',
    active: false,
    running: false,
    port: 0,
    installed: false,
    status: 'stopped',
    name: 'Node.js 20.11.1',
    pid: 0,
  },
]

const mockProjects = [
  {
    id: 'proj-demo',
    name: 'demo-app',
    type: 'php',
    root: 'C:/Projects/demo-app',
    domain: 'demo.test',
    port: 80,
    httpsPort: 443,
    ssl: false,
    environmentID: 'builtin-modern-php8',
    phpVersion: '8.3.12',
    mysqlVersion: '8.0.39',
    nginxVersion: '1.24.0',
    enabled: true,
    createdAt: '2024-01-15T10:00:00Z',
  },
]

const mockSystemInfo = {
  platform: 'windows',
  arch: 'amd64',
  appVersion: '0.2.0',
  installDir: 'C:/DevStack',
}

const mockSettings = {
  installDir: 'C:/DevStack',
  startOnBoot: false,
  minimizeToTray: true,
  theme: 'light',
  language: 'zh-CN',
  nginxConfig: {
    workerConnections: 2048,
    clientMaxBodySize: '20m',
    gzipEnabled: true,
    staticCacheDays: 7,
    keepaliveTimeout: 65,
    serverTokens: false,
  },
  phpConfig: {
    memoryLimit: '256M',
    maxExecutionTime: 120,
    uploadMaxFilesize: '20M',
    postMaxSize: '20M',
    displayErrors: true,
    opcacheEnabled: true,
    realpathCacheSize: '4k',
  },
  mysqlConfig: {
    innodbBufferPoolSize: '1G',
    innodbLogFileSize: '256M',
    innodbFlushLogAtTrxCommit: 2,
    maxConnections: 500,
    tmpTableSize: '64M',
    maxHeapTableSize: '64M',
    threadCacheSize: 100,
    tableOpenCache: 4000,
    bindAddress: '127.0.0.1',
    port: 3306,
  },
}

const mockOps = [
  {
    time: '2024-01-15 10:30:00',
    type: 'info',
    source: 'system',
    message: '浏览器预览模式 - 实际数据需在 Wails 环境中运行',
  },
]

const mockSnapshots = [
  { id: 'snap-1', name: '初始快照', createdAt: '2024-01-15 10:00:00', description: '环境初始化完成' },
]

// 浏览器模式下的空实现，避免报错
function mockFn(result: any = null) {
  return Promise.resolve(result)
}

function mockFnThrow(name: string) {
  return Promise.reject(new Error('浏览器预览模式不支持调用: ' + name))
}

// 安全调用：Wails 环境调用真实 API，浏览器环境返回 mock 数据
async function callOrMock<T>(fn: () => Promise<T>, mock: T): Promise<T> {
  if (isWailsEnv) {
    return fn()
  }
  return mock
}

export const api = {
  // ====== 服务管理 ======
  getServices: () => callOrMock(GetServices, mockServices as any),
  startService: (id: string) => callOrMock(() => StartService(id), { success: true } as any),
  stopService: (id: string) => callOrMock(() => StopService(id), { success: true } as any),
  restartService: (id: string) => callOrMock(() => RestartService(id), { success: true } as any),
  setServiceVersion: (id: string, version: string) =>
    callOrMock(() => SetServiceVersion(id, version), { success: true } as any),
  uninstallService: (id: string) => callOrMock(() => UninstallService(id), { success: true } as any),
  getServiceConfig: (id: string) => callOrMock(() => GetServiceConfig(id), mockSettings.nginxConfig as any),
  saveServiceConfig: (id: string, config: any) =>
    callOrMock(() => SaveServiceConfig(id, config), { success: true } as any),
  applyServiceConfig: (id: string) => callOrMock(() => ApplyServiceConfig(id), { success: true } as any),

  // ====== 运行时管理 ======
  getRuntimes: () => callOrMock(GetRuntimes, mockRuntimes as any),
  startRuntime: (id: string) => callOrMock(() => StartRuntime(id), { success: true } as any),
  stopRuntime: (id: string) => callOrMock(() => StopRuntime(id), { success: true } as any),
  getPHPRegistry: () => callOrMock(GetPHPRegistry, [] as any),
  refreshPHPRegistry: () => callOrMock(RefreshPHPRegistry, [] as any),
  getPHPExtensions: (version: string) => callOrMock(() => GetPHPExtensions(version), [] as any),
  savePHPExtensions: (version: string, exts: any) =>
    callOrMock(() => SavePHPExtensions(version, exts), { success: true } as any),

  // ====== 版本管理 ======
  getAvailableVersions: (serviceId: string) => callOrMock(() => GetAvailableVersions(serviceId), [] as any),
  getInstalledVersions: (serviceId: string) => callOrMock(() => GetInstalledVersions(serviceId), [] as any),
  checkVersionInstalled: (serviceId: string, version: string) =>
    callOrMock(() => CheckVersionInstalled(serviceId, version), false as any),
  downloadService: (serviceId: string, version: string) =>
    callOrMock(() => DownloadService(serviceId, version, ''), { success: true } as any),
  // 真正中止后端下载任务，key 形如 "download:nginx:1.24.0"
  cancelDownload: (key: string) => callOrMock(() => CancelDownload(key), undefined as any),
  uninstallVersion: (serviceId: string, version: string) =>
    callOrMock(() => UninstallVersion(serviceId, version), { success: true } as any),

  // ====== 项目管理 ======
  getProjects: () => callOrMock(GetProjects, mockProjects as any),
  createProject: (project: any) => callOrMock(() => CreateProject(project), { id: 'new-proj' } as any),
  updateProject: (project: any) => callOrMock(() => UpdateProject(project), { success: true } as any),
  deleteProject: (id: string) => callOrMock(() => DeleteProject(id), { success: true } as any),
  toggleProject: (id: string) => callOrMock(() => ToggleProject(id), { success: true } as any),
  activateEnvironmentForProject: (id: string) =>
    callOrMock(() => ActivateEnvironmentForProject(id), { success: true } as any),

  // ====== 环境管理 ======
  getEnvironment: () =>
    callOrMock(GetEnvironmentState, {
      services: {
        'nginx@1.24.0': { version: '1.24.0', state: 'running' },
        'mysql@8.0.39': { version: '8.0.39', state: 'running' },
      },
      runtimes: {
        'php@8.3.12': { version: '8.3.12', state: 'running', active: true },
      },
      projects: {},
      activeProfileID: 'builtin-modern-php8',
    } as any),
  activateEnvironment: (envId: string) => callOrMock(() => ActivateEnvironment(envId), { success: true } as any),
  activateEnvironmentOnly: (envId: string) =>
    callOrMock(() => ActivateEnvironmentOnly(envId), { success: true } as any),
  runEnvironment: (envId: string) => callOrMock(() => RunEnvironment(envId), { success: true } as any),
  deactivateEnvironment: () => callOrMock(() => DeactivateEnvironment(), { success: true } as any),
  logToast: (msg: string, type: string) => callOrMock(() => LogToast(msg, type), null as any),
  clearLogs: () => callOrMock(() => ClearLogs(), { success: true } as any),

  // ====== 环境 ======
  listEnvironments: () => callOrMock(ListEnvironments, mockEnvironments as any),
  listEnvironmentProfiles: () => callOrMock(ListEnvironmentProfiles, mockEnvironments as any),
  getOneEnvironment: (id: string) => callOrMock(() => GetEnvironment(id), mockEnvironments[0] as any),
  createEnvironment: (env: any) => callOrMock(() => CreateEnvironment(env), { id: 'new-env' } as any),
  updateEnvironment: (env: any) => callOrMock(() => UpdateEnvironment(env), { success: true } as any),
  deleteEnvironment: (id: string) => callOrMock(() => DeleteEnvironment(id), { success: true } as any),
  saveProjectAsEnvironment: (projectId: string, name?: string, description?: string) =>
    callOrMock(() => SaveProjectAsEnvironment({ projectId, name: name || '', description: description || '' }), {
      success: true,
    } as any),

  // ====== 快照管理 ======
  getSnapshots: () => callOrMock(GetSnapshots, mockSnapshots as any),
  getSnapshotDetail: (id: string) => callOrMock(() => GetSnapshotDetail(id), { id, content: '' } as any),
  deleteSnapshot: (id: string) => callOrMock(() => DeleteSnapshot(id), { success: true } as any),
  takeSnapshot: () => callOrMock(TakeSnapshot, 'snap-demo' as any),
  rollbackSnapshot: (id: string) => callOrMock(() => RollbackSnapshot(id), { success: true } as any),

  // ====== 设置 ======
  getSettings: () => callOrMock(GetSettings, mockSettings as any),
  saveSettings: (settings: any) => callOrMock(() => SaveSettings(settings), { success: true } as any),
  getConfigPath: () => callOrMock(GetConfigPath, 'C:/DevStack/config' as any),
  getSystemInfo: () => callOrMock(GetSystemInfo, mockSystemInfo as any),
  openDirectory: (path: string) => callOrMock(() => OpenDirectory(path), { success: true } as any),
  openFolder: (path: string) => callOrMock(() => OpenFolder(path), undefined as any),
  selectInstallDir: (defaultPath?: string) => callOrMock(() => SelectInstallDir(defaultPath || ''), '' as any),
  checkInstallDirChange: (target: string) =>
    callOrMock(() => CheckInstallDirChange(target), {
      currentDir: mockSettings.installDir,
      targetDir: target,
      valid: Boolean(target),
      errors: target ? [] : ['请选择新的安装目录'],
      warnings: [],
      targetExists: false,
      targetEmpty: false,
      sourceSize: 0,
      fileCount: 0,
      running: [],
      downloads: [],
    } as any),
  migrateInstallDir: (target: string, removeOld: boolean) =>
    callOrMock(() => MigrateInstallDir(target, removeOld), { installDir: target, warnings: [] } as any),
  onInstallDirProgress: (callback: (data: any) => void) => onWailsEvent('settings:migrate-progress', callback),
  checkSetupPorts: (services: string[]) => callOrMock(() => CheckSetupPorts(services), [] as any),
  completeSetup: (args: { installDir: string; services: string[] }) =>
    callOrMock(() => CompleteSetup(args), { success: true } as any),

  // ====== 日志 ======
  getRecentOps: (limit: number) => callOrMock(() => GetRecentOps(limit), mockOps as any),
  getLogs: (serviceId: string) => callOrMock(() => GetLogs(serviceId), [] as any),
  getLogFiles: () => callOrMock(GetLogFiles, [] as any),
  readLogFile: (file: string) => callOrMock(() => ReadLogFile(file), [] as any),
  getRecentLogLines: (lines: number) => callOrMock(() => GetRecentLogLines(lines), [] as any),

  // ====== MySQL ======
  checkMySQLRootAuth: () => callOrMock(CheckMySQLRootAuth, false as any),

  // ====== Setup Wizard ======
  // 事件订阅必须走 window.runtime（Wails 注入的 runtime 对象），window.go 下没有 runtime
  onSetupDownloadProgress: (callback: (data: any) => void) => onWailsEvent('setup:download-progress', callback),

  onSetupProgress: (callback: (data: any) => void) => onWailsEvent('setup:progress', callback),

  onSetupComplete: (callback: (data: any) => void) => onWailsEvent('setup:complete', callback),

  onSetupError: (callback: (data: any) => void) => onWailsEvent('setup:error', callback),

  onDownloadPhase: (callback: (data: any) => void) => onWailsEvent('download:phase', callback),

  // ====== Debug ======
  debugLog: (msg: string) => {
    if (isWailsEnv) {
      return DebugLog(msg)
    }
    return Promise.resolve()
  },
}

export type Api = typeof api
