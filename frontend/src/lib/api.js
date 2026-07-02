import {GetServices, StartService, StopService, RestartService, SetServiceVersion, UninstallService, CheckVersionInstalled, UninstallVersion} from '../../wailsjs/go/main/App.js'
import {GetSites, CreateSite, UpdateSite, DeleteSite, ToggleSite} from '../../wailsjs/go/main/App.js'
import {GetSettings, SaveSettings} from '../../wailsjs/go/main/App.js'
import {GetAvailableVersions, DownloadService, SaveServiceConfig} from '../../wailsjs/go/main/App.js'
import {GetSystemInfo, GetLogs, OpenDirectory} from '../../wailsjs/go/main/App.js'

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

  // Sites
  getSites: () => GetSites(),
  createSite: (site) => CreateSite(site),
  updateSite: (site) => UpdateSite(site),
  deleteSite: (id) => DeleteSite(id),
  toggleSite: (id) => ToggleSite(id),

  // Settings
  getSettings: () => GetSettings(),
  saveSettings: (s) => SaveSettings(s),

  // Downloads
  getAvailableVersions: (t) => GetAvailableVersions(t),
  downloadService: (t, v, url) => DownloadService(t, v, url),

  // Config
  saveServiceConfig: (t, vals) => SaveServiceConfig(t, vals),

  // System
  getSystemInfo: () => GetSystemInfo(),
  getLogs: (id) => GetLogs(id),
  openDirectory: (defaultPath) => OpenDirectory(defaultPath || ''),
}
