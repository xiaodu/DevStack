package main

import (
	"context"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/download"
	"github.com/xiaodu/DevStack/internal/log"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/project"
	"github.com/xiaodu/DevStack/internal/runtime"
	"github.com/xiaodu/DevStack/internal/service"
)

type App struct {
	ctx        context.Context
	cfg        *config.Config
	procMgr    *process.Manager
	svcMgr     *service.Manager
	rtMgr      *runtime.Manager
	projMgr    *project.Manager
	logWatcher *log.LogWatcher
	dlMgr      *download.Manager
	opStore    *log.OpStore
	AppVersion string
}

var serviceNames = map[string]string{
	"nginx": "Nginx", "mysql": "MySQL", "redis": "Redis", "php": "PHP",
}

var runtimeNames = map[string]string{
	"node": "Node.js", "python": "Python",
}

func NewApp() *App {
	cfg := config.NewConfig()
	procMgr := process.NewManager()
	svcMgr := service.NewManager(cfg, procMgr)

	installDir := cfg.Get().InstallDir
	if installDir != "" {
		config.EnsureDirs(installDir)
		runtime.AutoAssignPorts(cfg, installDir)
	}

	rtMgr := runtime.NewManager(cfg, procMgr)
	projMgr := project.NewManager(installDir, "1.26.0", rtMgr)
	logWatcher := log.NewLogWatcher()
	dlMgr := download.NewManager(installDir)
	opStore := log.NewOpStore(200)

	return &App{
		cfg:        cfg,
		procMgr:    procMgr,
		svcMgr:     svcMgr,
		rtMgr:      rtMgr,
		projMgr:    projMgr,
		logWatcher: logWatcher,
		dlMgr:      dlMgr,
		opStore:    opStore,
		AppVersion: "0.2.0",
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
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

func (a *App) GetServiceConfig(serviceType string) map[string]string {
	settings := a.cfg.Get()
	result := make(map[string]string)

	switch serviceType {
	case "nginx":
		nc := settings.NginxConfig
		result["workerConnections"] = fmt.Sprintf("%d", nc.WorkerConnections)
		result["clientMaxBodySize"] = nc.ClientMaxBodySize
		result["gzipEnabled"] = fmt.Sprintf("%v", nc.GzipEnabled)
		result["staticCacheDays"] = fmt.Sprintf("%d", nc.StaticCacheDays)
		result["keepaliveTimeout"] = fmt.Sprintf("%d", nc.KeepaliveTimeout)
		result["serverTokens"] = fmt.Sprintf("%v", nc.ServerTokens)
	case "php":
		pc := settings.PHPConfig
		result["memoryLimit"] = pc.MemoryLimit
		result["maxExecutionTime"] = fmt.Sprintf("%d", pc.MaxExecutionTime)
		result["uploadMaxFilesize"] = pc.UploadMaxFilesize
		result["postMaxSize"] = pc.PostMaxSize
		result["displayErrors"] = fmt.Sprintf("%v", pc.DisplayErrors)
		result["opcacheEnabled"] = fmt.Sprintf("%v", pc.OPcacheEnabled)
		result["realpathCacheSize"] = pc.RealpathCacheSize
	case "mysql":
		mc := settings.MySQLConfig
		result["innodbBufferPoolSize"] = mc.InnoDBBufferPoolSize
		result["innodbLogFileSize"] = mc.InnoDBLogFileSize
		result["innodbFlushLogAtTrxCommit"] = fmt.Sprintf("%d", mc.InnoDBFlushLogAtTrxCommit)
		result["maxConnections"] = fmt.Sprintf("%d", mc.MaxConnections)
		result["tmpTableSize"] = mc.TmpTableSize
		result["maxHeapTableSize"] = mc.MaxHeapTableSize
		result["threadCacheSize"] = fmt.Sprintf("%d", mc.ThreadCacheSize)
		result["tableOpenCache"] = fmt.Sprintf("%d", mc.TableOpenCache)
		result["bindAddress"] = mc.BindAddress
		result["port"] = fmt.Sprintf("%d", mc.Port)
	}

	return result
}

func (a *App) ApplyServiceConfig(serviceType string) error {
	if err := a.svcMgr.Configure(serviceType); err != nil {
		return err
	}
	// Reload nginx if it's running
	if serviceType == "nginx" {
		if svc := a.svcMgr.GetService("nginx"); svc != nil && svc.Info.Status == core.StatusRunning {
			return a.svcMgr.Restart("nginx")
		}
	}
	return nil
}

// ==================== Runtime APIs ====================

func (a *App) GetRuntimes() []core.RuntimeInfo {
	return a.rtMgr.List()
}

func (a *App) StartRuntime(id string) error {
	return nil
}

func (a *App) StopRuntime(id string) error {
	return nil
}

func (a *App) GetPHPRegistry() []runtime.PHPVersionInfo {
	return a.rtMgr.GetPHPRegistryVersions()
}

func (a *App) RefreshPHPRegistry() []runtime.PHPVersionInfo {
	return a.rtMgr.RefreshPHPRegistry()
}

func (a *App) GetPHPExtensions(version string) ([]runtime.PHPExtensionInfo, error) {
	return a.rtMgr.GetExtensions(version)
}

func (a *App) SavePHPExtensions(version string, extensions []string) error {
	return a.rtMgr.SaveExtensions(version, extensions)
}

// ==================== Project APIs ====================

func (a *App) GetProjects() []core.Project {
	return a.projMgr.List()
}

func (a *App) CreateProject(p core.Project) (*core.Project, error) {
	result, err := a.projMgr.Create(p)
	if err != nil {
		a.opStore.Add("创建项目", p.Name, p.Domain, "失败: "+err.Error())
	} else {
		a.opStore.Add("创建项目", p.Name, p.Domain, "成功")
	}
	return result, err
}

func (a *App) UpdateProject(p core.Project) error {
	err := a.projMgr.Update(p)
	if err != nil {
		a.opStore.Add("更新项目", p.Name, p.Domain, "失败: "+err.Error())
	} else {
		a.opStore.Add("更新项目", p.Name, p.Domain, "成功")
	}
	return err
}

func (a *App) DeleteProject(id string) error {
	projects := a.projMgr.List()
	var name, domain string
	for _, p := range projects {
		if p.ID == id {
			name, domain = p.Name, p.Domain
			break
		}
	}
	err := a.projMgr.Delete(id)
	if err != nil {
		a.opStore.Add("删除项目", name, domain, "失败: "+err.Error())
	} else {
		a.opStore.Add("删除项目", name, domain, "成功")
	}
	return err
}

func (a *App) ToggleProject(id string) error {
	projects := a.projMgr.List()
	var name, domain string
	var enabled bool
	for _, p := range projects {
		if p.ID == id {
			name, domain, enabled = p.Name, p.Domain, p.Enabled
			break
		}
	}
	err := a.projMgr.Toggle(id)
	action := "禁用项目"
	if !enabled {
		action = "启用项目"
	}
	if err != nil {
		a.opStore.Add(action, name, domain, "失败: "+err.Error())
	} else {
		a.opStore.Add(action, name, domain, "成功")
	}
	return err
}

// ==================== Legacy Site APIs (backward compat) ====================

func (a *App) GetSites() []core.Project {
	return a.projMgr.List()
}

func (a *App) CreateSite(s core.Project) (*core.Project, error) {
	return a.projMgr.Create(s)
}

func (a *App) UpdateSite(s core.Project) error {
	return a.projMgr.Update(s)
}

func (a *App) DeleteSite(id string) error {
	return a.projMgr.Delete(id)
}

func (a *App) ToggleSite(id string) error {
	return a.projMgr.Toggle(id)
}

// ==================== Settings APIs ====================

func (a *App) GetRecentOps(n int) []log.OpEntry {
	return a.opStore.Recent(n)
}

func (a *App) GetSettings() core.AppSettings {
	return a.cfg.Get()
}

func (a *App) SaveSettings(s core.AppSettings) error {
	return a.cfg.Update(s)
}

// GetConfigPath 返回配置文件的绝对路径，便于排查保存问题
func (a *App) GetConfigPath() string {
	return a.cfg.FilePath()
}

// ==================== MySQL APIs ====================

func (a *App) CheckMySQLRootAuth() (bool, error) {
	return a.svcMgr.CheckMySQLRootAuth()
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
		name := serviceNames[serviceType]
		if name == "" {
			name = runtimeNames[serviceType]
		}
		a.opStore.Add("下载", name, version, "失败: "+err.Error())
		return err
	}

	if svcNames, ok := serviceNames[serviceType]; ok && svcNames != "" {
		a.svcMgr.MarkInstalled(serviceType, version)
		a.opStore.Add("下载", serviceNames[serviceType], version, "成功")
		return a.svcMgr.Configure(serviceType)
	}

	a.opStore.Add("下载", runtimeNames[serviceType], version, "成功")
	return nil
}

// ==================== System APIs ====================

func (a *App) GetSystemInfo() core.SystemInfo {
	return core.SystemInfo{
		GoVersion:  goruntime.Version(),
		Platform:   goruntime.GOOS,
		Arch:       goruntime.GOARCH,
		AppVersion: a.AppVersion,
		InstallDir: a.cfg.Get().InstallDir,
	}
}

func (a *App) GetLogs(serviceID string) ([]log.LogEntry, error) {
	installDir := a.cfg.Get().InstallDir
	var logPath string

	if serviceID == "nginx" {
		svc := a.svcMgr.GetService(serviceID)
		if svc != nil {
			logPath = joinPath(installDir, "nginx", svc.Info.Version, "logs", "error.log")
		}
	} else if serviceID == "mysql" {
		svc := a.svcMgr.GetService(serviceID)
		if svc != nil {
			logPath = joinPath(installDir, "mysql", svc.Info.Version, "error.log")
		}
	} else if serviceID == "redis" {
		svc := a.svcMgr.GetService(serviceID)
		if svc != nil {
			logPath = joinPath(installDir, "redis", svc.Info.Version, "redis.log")
		}
	} else if strings.HasPrefix(serviceID, "php-") {
		version := strings.TrimPrefix(serviceID, "php-")
		logPath = joinPath(installDir, "php", version, "php_errors.log")
	}
	if logPath == "" {
		return []log.LogEntry{}, nil
	}
	entries, err := a.logWatcher.ReadRecent(logPath, 100)
	if err != nil {
		return []log.LogEntry{}, nil
	}
	return entries, nil
}

func (a *App) OpenDirectory(defaultPath string) (string, error) {
	path, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "选择目录",
		DefaultDirectory: defaultPath,
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
