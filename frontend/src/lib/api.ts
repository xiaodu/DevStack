import {
  ActivateEnvironment,
  ActivateEnvironmentForProject,
  ApplyEnvironment,
  ApplyServiceConfig,
  CheckMySQLRootAuth,
  CheckVersionInstalled,
  CreateEnvironmentProfile,
  CreateProject,
  DeactivateEnvironment,
  DeleteEnvironmentProfile,
  DeleteProject,
  DeleteSnapshot,
  DiffEnvironment,
  DownloadService,
  GetActiveEnvironment,
  GetAvailableVersions,
  GetConfigPath,
  GetEnvironment,
  GetEnvironmentProfile,
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
  OpenDirectory,
  ReadLogFile,
  RefreshPHPRegistry,
  RestartService,
  RollbackSnapshot,
  SavePHPExtensions,
  SaveProjectAsEnvironment,
  SaveServiceConfig,
  SaveSettings,
  SetServiceVersion,
  StartService,
  StopService,
  TakeSnapshot,
  ToggleProject,
  UninstallService,
  UninstallVersion,
  UpdateEnvironmentProfile,
  UpdateProject,
} from '../../wailsjs/go/main/App.js'

// 检测是否运行在 Wails 环境中（浏览器预览模式时 window.go 不存在）
const isWailsEnv = typeof window !== 'undefined' && (window as any).go !== undefined

// ====== Mock 数据（仅用于浏览器预览 UI 样式） ======
const mockProfiles = [
  {
    id: 'profile-lemp',
    name: 'LEMP 标准环境',
    description: 'Nginx + MySQL + PHP 8.3 组合，适用于现代 Web 应用',
    isBuiltin: true,
    services: [
      { id: 'nginx', version: '1.24.0' },
      { id: 'mysql', version: '8.0.36' },
    ],
    runtimes: [{ runtimeId: 'php', version: '8.3.0', default: true }],
    dependencies: {},
  },
  {
    id: 'profile-lnmp',
    name: 'LNMP 经典环境',
    description: 'Nginx + MySQL + PHP 7.4 组合，兼容老项目',
    isBuiltin: true,
    services: [
      { id: 'nginx', version: '1.24.0' },
      { id: 'mysql', version: '5.7.44' },
    ],
    runtimes: [{ runtimeId: 'php', version: '7.4.33', default: true }],
    dependencies: {},
  },
  {
    id: 'profile-lnmpj',
    name: 'LNMPJ 全栈环境',
    description: 'Nginx + MySQL + PHP + Node.js 组合，前后端一体化',
    isBuiltin: true,
    services: [
      { id: 'nginx', version: '1.24.0' },
      { id: 'mysql', version: '8.0.36' },
    ],
    runtimes: [
      { runtimeId: 'php', version: '8.3.0', default: true },
      { runtimeId: 'node', version: '20.11.1', default: false },
    ],
    dependencies: {},
  },
]

const mockServices = [
  {
    id: 'nginx',
    name: 'Nginx',
    type: 'webserver',
    category: 'web',
    version: '1.24.0',
    status: 'running',
    port: 80,
    pid: 1234,
    installed: true,
    autoStart: true,
  },
  {
    id: 'mysql',
    name: 'MySQL',
    type: 'database',
    category: 'db',
    version: '8.0.36',
    status: 'running',
    port: 3306,
    pid: 5678,
    installed: true,
    autoStart: true,
  },
  {
    id: 'redis',
    name: 'Redis',
    type: 'cache',
    category: 'cache',
    version: '7.2.4',
    status: 'stopped',
    port: 6379,
    pid: 0,
    installed: false,
    autoStart: false,
  },
]

const mockRuntimes = [
  {
    id: 'php-8.3.0',
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
    id: 'php-7.4.33',
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
    id: 'node-20.11.1',
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
    environmentID: 'profile-lemp',
    phpVersion: '8.3.0',
    mysqlVersion: '8.0.36',
    nginxVersion: '1.24.0',
    enabled: true,
    createdAt: '2024-01-15T10:00:00Z',
  },
]

const mockSystemInfo = {
  goVersion: 'go1.22.0',
  platform: 'windows',
  arch: 'amd64',
  cpu: 'Intel Core i7',
  memory: '16GB',
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
    callOrMock(() => DownloadService(serviceId, version), { success: true } as any),
  uninstallVersion: (serviceId: string, version: string) =>
    callOrMock(() => UninstallVersion(serviceId, version), { success: true } as any),

  // ====== 项目管理 ======
  getProjects: () => callOrMock(GetProjects, mockProjects as any),
  createProject: (project: any) => callOrMock(() => CreateProject(project), { id: 'new-proj' } as any),
  updateProject: (project: any) => callOrMock(() => UpdateProject(project), { success: true } as any),
  deleteProject: (id: string) => callOrMock(() => DeleteProject(id), { success: true } as any),
  toggleProject: (id: string) => callOrMock(() => ToggleProject(id), { success: true } as any),
  saveProjectAsEnvironment: (id: string) => callOrMock(() => SaveProjectAsEnvironment(id), { success: true } as any),
  activateEnvironmentForProject: (id: string) =>
    callOrMock(() => ActivateEnvironmentForProject(id), { success: true } as any),

  // ====== 环境管理 ======
  getEnvironment: () =>
    callOrMock(GetEnvironment, {
      services: {
        nginx: { version: '1.24.0', state: 'running' },
        mysql: { version: '8.0.36', state: 'running' },
      },
      runtimes: {
        'php-8.3.0': { version: '8.3.0', state: 'running', active: true },
      },
      projects: {},
      activeProfileID: 'profile-lemp',
    } as any),
  getActiveEnvironment: () =>
    callOrMock(GetActiveEnvironment, {
      services: {
        nginx: { version: '1.24.0', state: 'running' },
        mysql: { version: '8.0.36', state: 'running' },
      },
      runtimes: {
        'php-8.3.0': { version: '8.3.0', state: 'running', active: true },
      },
      projects: {},
      activeProfileID: 'profile-lemp',
    } as any),
  applyEnvironment: (env: any) => callOrMock(() => ApplyEnvironment(env), { success: true } as any),
  diffEnvironment: (env: any) => callOrMock(() => DiffEnvironment(env), [] as any),
  activateEnvironment: (profileId: string) =>
    callOrMock(() => ActivateEnvironment(profileId), { success: true } as any),
  deactivateEnvironment: () => callOrMock(() => DeactivateEnvironment(), { success: true } as any),

  // ====== 环境模板（Profiles） ======
  listEnvironmentProfiles: () => callOrMock(ListEnvironmentProfiles, mockProfiles as any),
  getEnvironmentProfile: (id: string) => callOrMock(() => GetEnvironmentProfile(id), mockProfiles[0] as any),
  createEnvironmentProfile: (profile: any) =>
    callOrMock(() => CreateEnvironmentProfile(profile), { id: 'new-profile' } as any),
  updateEnvironmentProfile: (profile: any) =>
    callOrMock(() => UpdateEnvironmentProfile(profile), { success: true } as any),
  deleteEnvironmentProfile: (id: string) => callOrMock(() => DeleteEnvironmentProfile(id), { success: true } as any),

  // ====== 快照管理 ======
  getSnapshots: () => callOrMock(GetSnapshots, mockSnapshots as any),
  getSnapshotDetail: (id: string) => callOrMock(() => GetSnapshotDetail(id), { id, content: '' } as any),
  takeSnapshot: () => callOrMock(TakeSnapshot, 'new-snap' as any),
  rollbackSnapshot: (id: string) => callOrMock(() => RollbackSnapshot(id), { success: true } as any),
  deleteSnapshot: (id: string) => callOrMock(() => DeleteSnapshot(id), { success: true } as any),

  // ====== 设置 ======
  getSettings: () => callOrMock(GetSettings, mockSettings as any),
  saveSettings: (settings: any) => callOrMock(() => SaveSettings(settings), { success: true } as any),
  getConfigPath: () => callOrMock(GetConfigPath, 'C:/DevStack/config' as any),
  getSystemInfo: () => callOrMock(GetSystemInfo, mockSystemInfo as any),
  openDirectory: (path: string) => callOrMock(() => OpenDirectory(path), { success: true } as any),

  // ====== 日志 ======
  getRecentOps: (limit: number) => callOrMock(() => GetRecentOps(limit), mockOps as any),
  getLogs: (serviceId: string) => callOrMock(() => GetLogs(serviceId), [] as any),
  getLogFiles: () => callOrMock(GetLogFiles, [] as any),
  readLogFile: (file: string) => callOrMock(() => ReadLogFile(file), [] as any),
  getRecentLogLines: (lines: number) => callOrMock(() => GetRecentLogLines(lines), [] as any),

  // ====== MySQL ======
  checkMySQLRootAuth: () => callOrMock(CheckMySQLRootAuth, false as any),
}

export type Api = typeof api
