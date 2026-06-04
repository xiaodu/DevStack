import {GetServices, StartService, StopService, RestartService, SetServiceVersion} from '../../wailsjs/go/main/App.js'
import {GetSites, CreateSite, UpdateSite, DeleteSite, ToggleSite} from '../../wailsjs/go/main/App.js'
import {GetSettings, SaveSettings} from '../../wailsjs/go/main/App.js'
import {GetAvailableVersions, DownloadService} from '../../wailsjs/go/main/App.js'
import {GetSystemInfo, GetLogs} from '../../wailsjs/go/main/App.js'

export const api = {
  // Services
  getServices: () => GetServices(),
  startService: (id) => StartService(id),
  stopService: (id) => StopService(id),
  restartService: (id) => RestartService(id),
  setServiceVersion: (id, version) => SetServiceVersion(id, version),

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

  // System
  getSystemInfo: () => GetSystemInfo(),
  getLogs: (id) => GetLogs(id),
}
