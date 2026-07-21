import {GetServices, StartService, StopService, RestartService, SetServiceVersion, UninstallService, CheckVersionInstalled, UninstallVersion} from '../../wailsjs/go/main/App.js'
import {GetSites, CreateSite, UpdateSite, DeleteSite, ToggleSite} from '../../wailsjs/go/main/App.js'
import {GetSettings, SaveSettings, GetConfigPath} from '../../wailsjs/go/main/App.js'
import {GetAvailableVersions, DownloadService, SaveServiceConfig, GetInstalledVersions, GetServiceConfig, ApplyServiceConfig} from '../../wailsjs/go/main/App.js'
import {GetSystemInfo, GetLogs, OpenDirectory} from '../../wailsjs/go/main/App.js'
import {GetRecentOps} from '../../wailsjs/go/main/App.js'
import {CheckMySQLRootAuth, GetPHPRegistry, RefreshPHPRegistry, GetPHPExtensions, SavePHPExtensions} from '../../wailsjs/go/main/App.js'
import {GetRuntimes, StartRuntime, StopRuntime} from '../../wailsjs/go/main/App.js'

export const api = {
  // Services
  getServices: () => GetServices(),
  startService: (id) => StartService(id),
  stopService: (id) => StopService(id),
  restartService: (id) => RestartService(id),
  setServiceVersion: (id, version) => SetServiceVersion(id, version),
  uninstallService: (id) => UninstallService(id),
  checkVersionInstalled: (t, v) => CheckVersionInstalled(t, v),
  uninstallVersion: (t, v) => UninstallVersion(t, v),

  // Sites (legacy name, returns Projects)
  getSites: () => GetSites(),
  createSite: (site) => CreateSite(site),
  updateSite: (site) => UpdateSite(site),
  deleteSite: (id) => DeleteSite(id),
  toggleSite: (id) => ToggleSite(id),

  // Runtimes
  getRuntimes: () => GetRuntimes(),
  startRuntime: (id) => StartRuntime(id),
  stopRuntime: (id) => StopRuntime(id),

  // Settings
  getSettings: () => GetSettings(),
  saveSettings: (s) => SaveSettings(s),
  getConfigPath: () => GetConfigPath(),

  // Downloads
  getAvailableVersions: (t) => GetAvailableVersions(t),
  getInstalledVersions: (t) => GetInstalledVersions(t),
  downloadService: (t, v, url) => DownloadService(t, v, url),

  // Config
  saveServiceConfig: (t, vals) => SaveServiceConfig(t, vals),
  getServiceConfig: (t) => GetServiceConfig(t),
  applyServiceConfig: (t) => ApplyServiceConfig(t),

  // MySQL
  checkMySQLRootAuth: () => CheckMySQLRootAuth(),

  // PHP
  getPHPRegistry: () => GetPHPRegistry(),
  refreshPHPRegistry: () => RefreshPHPRegistry(),
  getPHPExtensions: (ver) => GetPHPExtensions(ver),
  savePHPExtensions: (ver, exts) => SavePHPExtensions(ver, exts),

  // System
  getSystemInfo: () => GetSystemInfo(),
  getLogs: (id) => GetLogs(id),
  openDirectory: (defaultPath) => OpenDirectory(defaultPath || ''),
  getRecentOps: (n) => GetRecentOps(n || 50),
}
