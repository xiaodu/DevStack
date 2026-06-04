package main

import (
	"context"
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
	AppVersion string
}

func NewApp() *App {
	dataDir := getDefaultDataDir()
	cfg := config.NewConfig(dataDir)
	procMgr := process.NewManager()
	svcMgr := service.NewManager(cfg, procMgr)
	siteMgr := site.NewManager(dataDir)
	logWatcher := log.NewLogWatcher()
	dlMgr := download.NewManager(dataDir)

	return &App{
		cfg:        cfg,
		procMgr:    procMgr,
		svcMgr:     svcMgr,
		siteMgr:    siteMgr,
		logWatcher: logWatcher,
		dlMgr:      dlMgr,
		AppVersion: "0.2.0",
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func getDefaultDataDir() string {
	home, _ := gos.UserHomeDir()
	return filepath.Join(home, ".servbay")
}

// ==================== Service APIs ====================

func (a *App) GetServices() []core.ServiceInfo {
	return a.svcMgr.List()
}

func (a *App) StartService(id string) error {
	return a.svcMgr.Start(id)
}

func (a *App) StopService(id string) error {
	return a.svcMgr.Stop(id)
}

func (a *App) RestartService(id string) error {
	return a.svcMgr.Restart(id)
}

func (a *App) SetServiceVersion(id, version string) error {
	return a.svcMgr.SetVersion(id, version)
}

// ==================== Site APIs ====================

func (a *App) GetSites() []core.Site {
	return a.siteMgr.List()
}

func (a *App) CreateSite(s core.Site) (*core.Site, error) {
	return a.siteMgr.Create(s)
}

func (a *App) UpdateSite(s core.Site) error {
	return a.siteMgr.Update(s)
}

func (a *App) DeleteSite(id string) error {
	return a.siteMgr.Delete(id)
}

func (a *App) ToggleSite(id string) error {
	return a.siteMgr.Toggle(id)
}

// ==================== Settings APIs ====================

func (a *App) GetSettings() core.AppSettings {
	return a.cfg.Get()
}

func (a *App) SaveSettings(s core.AppSettings) error {
	return a.cfg.Update(s)
}

// ==================== Download APIs ====================

func (a *App) GetAvailableVersions(serviceType string) ([]download.ReleaseInfo, error) {
	return a.dlMgr.GetAvailableVersions(serviceType)
}

func (a *App) DownloadService(serviceType, version, url string) error {
	installDir := a.cfg.Get().InstallDir
	destDir := joinPath(installDir, serviceType, version)
	destPath := joinPath(destDir, version+".zip")
	return a.dlMgr.Download(url, destPath, nil)
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

func (a *App) OpenDirectory(path string) error {
	_, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "选择目录",
	})
	return err
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
