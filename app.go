package main

import (
	"context"
	"fmt"
	gos "os"
	"path/filepath"
	goruntime "runtime"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/download"
	"github.com/xiaodu/DevStack/internal/log"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/service"
	"github.com/xiaodu/DevStack/internal/site"
)

type App struct {
	ctx        context.Context
	cfg        *config.Config
	procMgr    *process.Manager
	svcMgr     *service.Manager
	siteMgr    *site.Manager
	logWatcher *log.LogWatcher
	dlMgr      *download.Manager
	opStore    *log.OpStore
	AppVersion string
}

var serviceNames = map[string]string{
	"nginx": "Nginx", "mysql": "MySQL", "redis": "Redis",
	"php": "PHP", "node": "Node.js", "python": "Python",
}

func NewApp() *App {
	dataDir := getDefaultDataDir()
	cfg := config.NewConfig(dataDir)
	procMgr := process.NewManager()
	svcMgr := service.NewManager(cfg, procMgr)

	installDir := cfg.Get().InstallDir
	if installDir != "" {
		service.AutoAssignPorts(cfg, installDir)
	}

	siteMgr := site.NewManager(dataDir, installDir, "1.26.0", 9000)
	logWatcher := log.NewLogWatcher()
	dlMgr := download.NewManager(dataDir)
	opStore := log.NewOpStore(200)

	return &App{
		cfg:        cfg,
		procMgr:    procMgr,
		svcMgr:     svcMgr,
		siteMgr:    siteMgr,
		logWatcher: logWatcher,
		dlMgr:      dlMgr,
		opStore:    opStore,
		AppVersion: "0.2.0",
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func getDefaultDataDir() string {
	home, _ := gos.UserHomeDir()
	return filepath.Join(home, ".devstack")
}

// ==================== Service APIs ====================

func (a *App) GetServices() []core.ServiceInfo {
	return a.svcMgr.List()
}

func (a *App) StartService(id string) error {
	err := a.svcMgr.Start(id)
	name := serviceNames[id]
	if name == "" {
		name = id
	}
	if err != nil {
		a.opStore.Add("启动", name, "", "失败: "+err.Error())
	} else {
		a.opStore.Add("启动", name, "", "成功")
	}
	return err
}

func (a *App) StopService(id string) error {
	err := a.svcMgr.Stop(id)
	name := serviceNames[id]
	if name == "" {
		name = id
	}
	if err != nil {
		a.opStore.Add("停止", name, "", "失败: "+err.Error())
	} else {
		a.opStore.Add("停止", name, "", "成功")
	}
	return err
}

func (a *App) RestartService(id string) error {
	err := a.svcMgr.Restart(id)
	name := serviceNames[id]
	if name == "" {
		name = id
	}
	if err != nil {
		a.opStore.Add("重启", name, "", "失败: "+err.Error())
	} else {
		a.opStore.Add("重启", name, "", "成功")
	}
	return err
}

func (a *App) SetServiceVersion(id, version string) error {
	return a.svcMgr.SetVersion(id, version)
}

func (a *App) UninstallService(id string) error {
	return a.svcMgr.Uninstall(id)
}

func (a *App) CheckVersionInstalled(serviceType, version string) bool {
	return a.svcMgr.CheckVersionInstalled(serviceType, version)
}

func (a *App) UninstallVersion(serviceType, version string) error {
	return a.svcMgr.UninstallVersion(serviceType, version)
}

func (a *App) ConfigureService(serviceType string) error {
	return a.svcMgr.Configure(serviceType)
}

func (a *App) SaveServiceConfig(serviceType string, values map[string]string) error {
	return a.svcMgr.SaveConfig(serviceType, values)
}

// ==================== Site APIs ====================

func (a *App) GetSites() []core.Site {
	return a.siteMgr.List()
}

func (a *App) CreateSite(s core.Site) (*core.Site, error) {
	result, err := a.siteMgr.Create(s)
	if err != nil {
		a.opStore.Add("创建站点", s.Name, s.Domain, "失败: "+err.Error())
	} else {
		a.opStore.Add("创建站点", s.Name, s.Domain, "成功")
	}
	return result, err
}

func (a *App) UpdateSite(s core.Site) error {
	err := a.siteMgr.Update(s)
	if err != nil {
		a.opStore.Add("更新站点", s.Name, s.Domain, "失败: "+err.Error())
	} else {
		a.opStore.Add("更新站点", s.Name, s.Domain, "成功")
	}
	return err
}

func (a *App) DeleteSite(id string) error {
	sites := a.siteMgr.List()
	var name, domain string
	for _, s := range sites {
		if s.ID == id {
			name, domain = s.Name, s.Domain
			break
		}
	}
	err := a.siteMgr.Delete(id)
	if err != nil {
		a.opStore.Add("删除站点", name, domain, "失败: "+err.Error())
	} else {
		a.opStore.Add("删除站点", name, domain, "成功")
	}
	return err
}

func (a *App) ToggleSite(id string) error {
	sites := a.siteMgr.List()
	var name, domain string
	var enabled bool
	for _, s := range sites {
		if s.ID == id {
			name, domain, enabled = s.Name, s.Domain, s.Enabled
			break
		}
	}
	err := a.siteMgr.Toggle(id)
	action := "禁用站点"
	if !enabled {
		action = "启用站点"
	}
	if err != nil {
		a.opStore.Add(action, name, domain, "失败: "+err.Error())
	} else {
		a.opStore.Add(action, name, domain, "成功")
	}
	return err
}

func (a *App) GetRecentOps(n int) []log.OpEntry {
	return a.opStore.Recent(n)
}

// ==================== Settings APIs ====================

func (a *App) GetSettings() core.AppSettings {
	return a.cfg.Get()
}

func (a *App) SaveSettings(s core.AppSettings) error {
	return a.cfg.Update(s)
}

// ==================== MySQL APIs ====================

func (a *App) CheckMySQLRootAuth() (bool, error) {
	return a.svcMgr.CheckMySQLRootAuth()
}

// ==================== PHP APIs ====================

func (a *App) GetPHPExtensions(version string) ([]service.PHPExtensionInfo, error) {
	return a.svcMgr.GetPHPExtensions(version)
}

func (a *App) SavePHPExtensions(version string, extensions []string) error {
	return a.svcMgr.SavePHPExtensions(version, extensions)
}

func (a *App) GetPHPRegistry() []service.PHPVersionInfo {
	reg := service.LoadPHPRegistry(a.cfg)
	return reg.Versions
}

func (a *App) RefreshPHPRegistry() []service.PHPVersionInfo {
	installDir := a.cfg.Get().InstallDir
	if installDir != "" {
		service.AutoAssignPorts(a.cfg, installDir)
	}
	reg := service.LoadPHPRegistry(a.cfg)
	return reg.Versions
}

// ==================== Download APIs ====================

func (a *App) GetAvailableVersions(serviceType string) ([]download.ReleaseInfo, error) {
	return a.dlMgr.GetAvailableVersions(serviceType)
}

func (a *App) GetInstalledVersions(serviceType string) []string {
	return a.svcMgr.GetInstalledVersions(serviceType)
}

func (a *App) DownloadService(serviceType, version, url string) error {
	installDir := a.cfg.Get().InstallDir
	if installDir == "" {
		return fmt.Errorf("安装目录未设置，请先完成初始化设置")
	}

	var urls []string
	if url != "" {
		urls = append(urls, url)
	}
	releases, _ := a.dlMgr.GetAvailableVersions(serviceType)
	for _, r := range releases {
		if r.Version == version {
			if r.MirrorURL != "" {
				urls = append(urls, r.MirrorURL)
			}
			if url == "" && r.URL != "" {
				urls = append(urls, r.URL)
			}
			break
		}
	}
	if len(urls) == 0 {
		return fmt.Errorf("未找到 %s %s 的下载地址", serviceType, version)
	}

	destDir := joinPath(installDir, serviceType, version)
	if err := a.dlMgr.TryDownloadAndExtract(urls, destDir, nil); err != nil {
		a.opStore.Add("下载", serviceNames[serviceType], version, "失败: "+err.Error())
		return err
	}
	a.svcMgr.MarkInstalled(serviceType, version)
	a.opStore.Add("下载", serviceNames[serviceType], version, "成功")
	return a.svcMgr.Configure(serviceType)
}

// ==================== System APIs ====================

func (a *App) GetSystemInfo() core.SystemInfo {
	return core.SystemInfo{
	GoVersion:  goruntime.Version(),
	Platform:   goruntime.GOOS,
	Arch:       goruntime.GOARCH,
		AppVersion: a.AppVersion,
		InstallDir: a.cfg.Get().InstallDir,
		DataDir:    a.cfg.Get().DataDir,
	}
}

func (a *App) GetLogs(serviceID string) ([]log.LogEntry, error) {
	installDir := a.cfg.Get().InstallDir
	logPath := joinPath(installDir, serviceID, "logs", "error.log")
	return a.logWatcher.ReadRecent(logPath, 100)
}

func (a *App) OpenDirectory(defaultPath string) (string, error) {
	path, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:                "选择目录",
		DefaultDirectory:     defaultPath,
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) ShowMessage(title, message string) {
	wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
		Title:   title,
		Message: message,
	})
}

func joinPath(elem ...string) string {
	return filepath.Join(elem...)
}
