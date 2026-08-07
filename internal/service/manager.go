package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/provider"
	"github.com/xiaodu/DevStack/internal/registry"
	"github.com/xiaodu/DevStack/internal/resource"
	"github.com/xiaodu/DevStack/internal/reactive"
	"github.com/xiaodu/DevStack/internal/tx"
)

type Manager struct {
	mu        sync.RWMutex
	cfg       *config.Config
	procMgr   *process.Manager
	resMgr    *resource.Manager
	services  map[string]*ServiceInstance
	dependents map[string][]string
	reactive  *reactive.Engine
}

type ServiceInstance struct {
	Info         core.ServiceInfo
	process      *process.ProcessHandle
	stopped      chan struct{}
	restartCount int
}

func NewManager(cfg *config.Config, procMgr *process.Manager, resMgr *resource.Manager, reactiveEng *reactive.Engine) *Manager {
	m := &Manager{
		cfg:        cfg,
		procMgr:    procMgr,
		resMgr:     resMgr,
		services:   make(map[string]*ServiceInstance),
		dependents: make(map[string][]string),
		reactive:   reactiveEng,
	}
	m.registerDefaults()
	m.registerReactiveRules()
	return m
}

func (m *Manager) registerReactiveRules() {
	m.reactive.AddRule(reactive.NewRule(
		"service-crash-restart",
		reactive.EventServiceCrashed,
		func(ctx context.Context, event *reactive.Event) bool {
			if count, ok := event.Data["restartCount"].(int); ok && count >= maxRestartCount {
				return false
			}
			return true
		},
		func(ctx context.Context, event *reactive.Event) error {
			if id, ok := event.Data["serviceID"].(string); ok {
				return m.Start(id)
			}
			return nil
		},
	))

	m.reactive.AddRule(reactive.NewRule(
		"service-health-failed",
		reactive.EventServiceHealthFailed,
		func(ctx context.Context, event *reactive.Event) bool {
			return true
		},
		func(ctx context.Context, event *reactive.Event) error {
			if id, ok := event.Data["serviceID"].(string); ok {
				m.publishCrashEvent(id)
			}
			return nil
		},
	))
}

func kindFromID(id string) string {
	if idx := strings.IndexByte(id, '@'); idx > 0 {
		return id[:idx]
	}
	return id
}

func (m *Manager) registerDefaults() {
	installDir := m.cfg.Get().InstallDir
	manifest := registry.Builtin()

	for _, pkg := range manifest.List("") {
		svcType := serviceTypeFromPkg(pkg.Type)
		if svcType == "" || svcType == core.TypePHP {
			continue
		}
		cat := serviceCategoryFromPkg(pkg.Category)

		svcDir := filepath.Join(installDir, "programs", string(svcType))
		entries, err := os.ReadDir(svcDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			version := entry.Name()
			verDir := filepath.Join(svcDir, version)
			if !dirExists(verDir) {
				continue
			}
			id := string(svcType) + "@" + version
			m.services[id] = &ServiceInstance{
				Info: core.ServiceInfo{
					ID: id, Kind: string(svcType), Name: pkg.Name,
					Type: svcType, Category: cat,
					Version: version, Port: pkg.DefaultPort,
					Status: core.StatusStopped, Installed: true,
				},
			}
		}
	}
	for _, s := range m.services {
		pid := process.FindPIDByPort(s.Info.Port)
		if pid > 0 {
			s.Info.Status = core.StatusRunning
			s.Info.PID = pid
		}
	}
}

var serviceOrder = []string{"nginx", "apache", "mysql", "postgresql", "mongodb", "redis", "memcached", "node", "python", "composer"}

func (m *Manager) matchConfigHandler(id string) ConfigHandler {
	kind := kindFromID(id)
	if h, ok := configHandlers[kind]; ok {
		return h
	}
	return nil
}

func isPortAlive(port, pid int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return process.IsProcessRunning(pid)
}

func (m *Manager) syncServiceState(s *ServiceInstance, portPIDMap map[int]int) core.ServiceInfo {
	info := s.Info

	switch info.Status {
	case core.StatusStarting, core.StatusStopping:
		// Keep transitional states as-is

	case core.StatusRunning:
		stillRunning := false
		if s.process != nil && s.process.PID > 0 && m.procMgr.IsRunning(s.process.PID) {
			stillRunning = true
		}
		if !stillRunning && info.Port > 0 {
			if pid, ok := portPIDMap[info.Port]; ok {
				if isPortAlive(info.Port, pid) {
					stillRunning = true
					info.PID = pid
				}
			}
		}
		if !stillRunning {
			info.Status = core.StatusStopped
			info.PID = 0
		}

	case core.StatusStopped:
		if info.Port > 0 {
			if pid, ok := portPIDMap[info.Port]; ok {
				if isPortAlive(info.Port, pid) {
					info.Status = core.StatusRunning
					info.PID = pid
				}
			}
		}
	}

	if info.Status == core.StatusRunning && s.process != nil {
		info.Uptime = time.Since(s.process.Started).Round(time.Second).String()
	}

	return info
}

func (m *Manager) List() []core.ServiceInfo {
	m.mu.RLock()
	servicesCopy := make(map[string]*ServiceInstance, len(m.services))
	for id, s := range m.services {
		servicesCopy[id] = s
	}
	m.mu.RUnlock()

	portPIDMap := process.FindAllPIDsByPort()

	orderMap := make(map[string]int, len(serviceOrder))
	for i, kind := range serviceOrder {
		orderMap[kind] = i
	}
	type entry struct {
		info core.ServiceInfo
		kind string
	}
	entries := make([]entry, 0, len(servicesCopy))
	for _, s := range servicesCopy {
		info := m.syncServiceState(s, portPIDMap)
		entries = append(entries, entry{info: info, kind: kindFromID(info.ID)})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		oi, oki := orderMap[entries[i].kind]
		oj, okj := orderMap[entries[j].kind]
		if !oki { oi = len(serviceOrder) }
		if !okj { oj = len(serviceOrder) }
		if oi != oj { return oi < oj }
		return entries[i].info.ID < entries[j].info.ID
	})
	result := make([]core.ServiceInfo, len(entries))
	for i, e := range entries {
		result[i] = e.info
	}

	// 同端口多个版本只会有一个真正在运行：通过监听进程的可执行文件路径判定归属版本。
	return m.resolvePortConflicts(result, portPIDMap)
}

// resolvePortConflicts 同一端口被多个版本的服务实例占用时（如 nginx 1.20/1.24/1.26 共享 80 端口），
// 只允许可执行文件路径匹配到安装目录的版本显示"运行中"，其余版本保持"已停止"。
func (m *Manager) resolvePortConflicts(infos []core.ServiceInfo, portPIDMap map[int]int) []core.ServiceInfo {
	type portGroup struct {
		pid int
		idx []int
	}
	groups := make(map[int]*portGroup)
	for i := range infos {
		info := &infos[i]
		if info.Status != core.StatusRunning || info.Port <= 0 {
			continue
		}
		g := groups[info.Port]
		if g == nil {
			g = &portGroup{pid: portPIDMap[info.Port]}
			groups[info.Port] = g
		}
		g.idx = append(g.idx, i)
	}

	installDir := m.cfg.Get().InstallDir
	for _, g := range groups {
		if len(g.idx) < 2 || g.pid <= 0 {
			continue
		}
		owner := -1
		for _, i := range g.idx {
			info := &infos[i]
			if exePathMatchesPID(g.pid, installDir, info.Kind, info.Version) {
				owner = i
				break
			}
		}
		if owner < 0 {
			// 无法匹配（进程已退出或 wmic 失败）：保留排序靠前的版本
			owner = g.idx[0]
		}
		for _, i := range g.idx {
			if i == owner {
				continue
			}
			infos[i].Status = core.StatusStopped
			infos[i].PID = 0
		}
	}
	return infos
}

// exePathMatchesPID 判断端口监听进程的 exe 路径是否落在 installDir/programs/<kind>/<version> 目录下。
func exePathMatchesPID(pid int, installDir, kind, version string) bool {
	exe := process.GetExecutablePath(pid)
	if exe == "" {
		return false
	}
	installPath := strings.ToLower(filepath.Join(installDir, "programs", kind, version))
	return strings.Contains(strings.ToLower(exe), installPath)
}

func (m *Manager) Get(id string) (core.ServiceInfo, error) {
	m.mu.RLock()
	s, ok := m.services[id]
	if !ok {
		m.mu.RUnlock()
		return core.ServiceInfo{}, core.ErrServiceNotFound
	}
	m.mu.RUnlock()

	portPIDMap := process.FindAllPIDsByPort()
	info := m.syncServiceState(s, portPIDMap)

	return info, nil
}

func (m *Manager) Start(id string) error {
	m.mu.Lock()

	s, ok := m.services[id]
	if !ok {
		m.mu.Unlock()
		return core.ErrServiceNotFound
	}
	if s.Info.Status == core.StatusRunning {
		m.mu.Unlock()
		return nil
	}

	kind := kindFromID(id)
	version := s.Info.Version
	port := s.Info.Port
	s.Info.Status = core.StatusStarting
	m.mu.Unlock()

	p := provider.Get(kind)
	if p == nil {
		return fmt.Errorf("不支持的的服务类型: %s", id)
	}

	config := map[string]interface{}{
		"version": version,
		"port":    port,
	}
	entity, err := p.NewInstance(context.Background(), config)
	if err != nil {
		m.mu.Lock()
		if si, ok := m.services[id]; ok {
			si.Info.Status = core.StatusStopped
		}
		m.mu.Unlock()
		return err
	}
	if err := entity.Start(context.Background()); err != nil {
		m.mu.Lock()
		if si, ok := m.services[id]; ok {
			si.Info.Status = core.StatusStopped
		}
		m.mu.Unlock()
		return err
	}
	pid := process.FindPIDByPort(port)
	proc := &process.ProcessHandle{PID: pid, Started: time.Now()}
	if pid <= 0 {
		proc = nil
	}

	m.mu.Lock()
	s = m.services[id]
	if s == nil {
		m.mu.Unlock()
		return core.ErrServiceNotFound
	}
	s.Info.Status = core.StatusRunning
	if proc != nil && proc.PID > 0 {
		s.Info.PID = proc.PID
	}
	s.process = proc
	s.stopped = make(chan struct{})
	s.restartCount = 0

	stoppedChan := s.stopped
	m.mu.Unlock()

	if proc != nil {
		go m.watchProcess(id, stoppedChan)
	}
	return nil
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()

	s, ok := m.services[id]
	if !ok {
		m.mu.Unlock()
		return core.ErrServiceNotFound
	}

	if dependents := m.dependents[id]; len(dependents) > 0 {
		m.mu.Unlock()
		return fmt.Errorf("服务 %s 被以下实体依赖，无法停止: %v", id, dependents)
	}

	if s.stopped != nil {
		close(s.stopped)
		s.stopped = nil
	}

	kind := kindFromID(id)
	version := s.Info.Version
	port := s.Info.Port

	m.mu.Unlock()

	// 在锁外执行外部进程停止操作
	p := provider.Get(kind)
	if p != nil {
		config := map[string]interface{}{
			"version": version,
			"port":    port,
		}
		entity, err := p.NewInstance(context.Background(), config)
		if err != nil {
			return err
		}
		if err := entity.Stop(context.Background()); err != nil {
			return err
		}
	} else {
		pid := process.FindPIDByPort(port)
		if pid > 0 {
			m.mu.Lock()
			if si, ok := m.services[id]; ok {
				si.Info.Status = core.StatusStopping
			}
			m.mu.Unlock()
			m.procMgr.Stop(pid)
		}
	}

	// 兜底：端口仍被占用时强制终止
	if port > 0 {
		if pid := process.FindPIDByPort(port); pid > 0 {
			m.procMgr.Stop(pid)
		}
		for j := 0; j < 6; j++ {
			time.Sleep(500 * time.Millisecond)
			if process.FindPIDByPort(port) == 0 {
				break
			}
		}
		if pid := process.FindPIDByPort(port); pid > 0 {
			return fmt.Errorf("服务 %s 停止失败: 端口 %d 仍被进程 %d 占用", id, port, pid)
		}
	}

	// 更新状态
	m.mu.Lock()
	if si, ok := m.services[id]; ok {
		si.process = nil
		si.Info.Status = core.StatusStopped
		si.Info.PID = 0
	}
	m.mu.Unlock()
	return nil
}

const maxRestartCount = 3

func (m *Manager) watchProcess(id string, stopped <-chan struct{}) {
	for {
		select {
		case <-stopped:
			return
		case <-time.After(5 * time.Second):
			m.mu.RLock()
			s2 := m.services[id]
			if s2 == nil || s2.stopped == nil || s2.Info.Status != core.StatusRunning {
				m.mu.RUnlock()
				return
			}
			port := s2.Info.Port
			m.mu.RUnlock()

			if !m.checkServiceHealth(id, port) {
				time.Sleep(2 * time.Second)
				if !m.checkServiceHealth(id, port) {
					m.mu.Lock()
					s3 := m.services[id]
					if s3 != nil {
						s3.restartCount++
						if s3.restartCount > maxRestartCount {
							s3.process = nil
							s3.Info.PID = 0
							s3.Info.Status = core.StatusError
							s3.Info.Error = fmt.Sprintf("服务连续崩溃 %d 次，已停止自动重启", maxRestartCount)
							m.mu.Unlock()
							m.reactive.Publish(reactive.NewEvent(
								reactive.EventServiceCrashed,
								id,
								map[string]interface{}{
									"serviceID":   id,
									"restartCount": s3.restartCount,
									"maxRetries":   maxRestartCount,
									"error":        s3.Info.Error,
								},
							))
							return
						}
						m.mu.Unlock()

						m.reactive.Publish(reactive.NewEvent(
							reactive.EventServiceHealthFailed,
							id,
							map[string]interface{}{
								"serviceID":   id,
								"restartCount": s3.restartCount,
							},
						))
					} else {
						m.mu.Unlock()
					}
				}
			}
		}
	}
}

func (m *Manager) publishCrashEvent(id string) {
	m.mu.RLock()
	s := m.services[id]
	if s == nil {
		m.mu.RUnlock()
		return
	}
	restartCount := s.restartCount
	m.mu.RUnlock()

	backoff := time.Duration(5*restartCount) * time.Second
	time.Sleep(backoff)

	m.mu.Lock()
	s3 := m.services[id]
	if s3 != nil && s3.stopped != nil && s3.Info.Status == core.StatusRunning {
		s3.process = nil
		s3.Info.PID = 0
	}
	m.mu.Unlock()

	m.reactive.Publish(reactive.NewEvent(
		reactive.EventServiceCrashed,
		id,
		map[string]interface{}{
			"serviceID":   id,
			"restartCount": restartCount,
		},
	))
}

func (m *Manager) checkServiceHealth(id string, port int) bool {
	if port <= 0 {
		return true
	}

	if s := m.GetService(id); s != nil && s.process != nil {
		pids := append([]int{s.process.PID}, s.process.ExtraPIDs...)
		for _, pid := range pids {
			if pid > 0 && !m.procMgr.IsRunning(pid) {
				return false
			}
		}
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (m *Manager) Restart(id string) error {
	if err := m.Stop(id); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return m.Start(id)
}

func (m *Manager) Uninstall(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.services[id]
	if !ok {
		return core.ErrServiceNotFound
	}

	if s.stopped != nil {
		close(s.stopped)
		s.stopped = nil
	}

	if s.Info.PID > 0 {
		m.procMgr.Stop(s.Info.PID)
		s.Info.PID = 0
		s.process = nil
	}

	installDir := m.cfg.Get().InstallDir
	binPath := filepath.Join(installDir, "programs", kindFromID(id), s.Info.Version)
	if dirExists(binPath) {
		if err := removeAll(id, binPath); err != nil {
			return fmt.Errorf("删除安装目录失败: %w", err)
		}
	}

	s.Info.Installed = false
	s.Info.Status = core.StatusStopped
	return nil
}

// SetVersion registers a version for a given service kind.
// In the kind@version model, this is effectively a no-op since
// version is baked into the ID. Kept for compatibility with engine.
func (m *Manager) SetVersion(id, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entryID := kindFromID(id) + "@" + version
	if _, ok := m.services[entryID]; !ok {
		return core.ErrServiceNotFound
	}
	return nil
}

var kindNames = map[string]string{
	"nginx": "Nginx", "mysql": "MySQL", "redis": "Redis",
	"memcached": "Memcached", "apache": "Apache", "mongodb": "MongoDB",
	"postgresql": "PostgreSQL", "composer": "Composer",
}

func (m *Manager) MarkInstalled(id, version string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	kind := kindFromID(id)
	if kind == "php" {
		return
	}
	entryID := kind + "@" + version
	if s, ok := m.services[entryID]; ok {
		s.Info.Installed = true
		return
	}
	m.services[entryID] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: entryID, Kind: kind, Name: kindNames[kind],
			Type: core.ServiceType(kind),
			Version: version, Installed: true,
			Status: core.StatusStopped,
		},
	}
}

func (m *Manager) SaveConfig(id string, values map[string]string) error {
	handler := m.matchConfigHandler(id)
	if handler == nil {
		return nil
	}
	if kindFromID(id) == "php" {
		// PHP 属运行时，配置为全局（PHPSettings），不依赖 ServiceInstance
		return handler.SaveConfig(nil, m.cfg, values)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.services[id]
	if !ok {
		return core.ErrServiceNotFound
	}
	return handler.SaveConfig(s, m.cfg, values)
}

func (m *Manager) Configure(id string) error {
	m.mu.Lock()
	s, ok := m.services[id]
	if !ok {
		kind := kindFromID(id)
		for _, cand := range m.services {
			if cand.Info.Kind == kind {
				s = cand
				ok = true
				break
			}
		}
	}
	if !ok {
		m.mu.Unlock()
		return core.ErrServiceNotFound
	}
	handler := m.matchConfigHandler(id)
	if handler == nil {
		m.mu.Unlock()
		return nil
	}
	oldVersion := s.Info.Version
	m.mu.Unlock()

	compensator := tx.NewCompensator()
	txn := tx.NewTransaction("configure-" + id)

	installDir := m.cfg.Get().InstallDir
	configPath := filepath.Join(installDir, "programs", kindFromID(id), oldVersion, "conf")
	backupPath := configPath + ".bak"

	txn.
		Step("backup-config", func(ctx context.Context) error {
			return copyDir(configPath, backupPath)
		}, compensator.RestoreBackup(backupPath, configPath)).
		Step("apply-config", func(ctx context.Context) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			return handler.Configure(m.services[id], m.cfg, m.resMgr)
		}, compensator.RestoreBackup(backupPath, configPath))

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("配置事务失败: %w", err)
	}

	os.RemoveAll(backupPath)
	return nil
}

func copyDir(src, dest string) error {
	if !dirExists(src) {
		return nil
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return copyDirRecursive(src, dest)
}

func copyDirRecursive(src, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		destPath := filepath.Join(dest, entry.Name())
		if entry.IsDir() {
			if err := copyDirRecursive(srcPath, destPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(destPath, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) RegisterDependent(serviceID, dependentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dependents == nil {
		m.dependents = make(map[string][]string)
	}
	for _, d := range m.dependents[serviceID] {
		if d == dependentID {
			return
		}
	}
	m.dependents[serviceID] = append(m.dependents[serviceID], dependentID)
}

func (m *Manager) UnregisterDependent(serviceID, dependentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dependents := m.dependents[serviceID]
	for i, d := range dependents {
		if d == dependentID {
			m.dependents[serviceID] = append(dependents[:i], dependents[i+1:]...)
			return
		}
	}
}

func (m *Manager) GetDependents(serviceID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	deps := make([]string, len(m.dependents[serviceID]))
	copy(deps, m.dependents[serviceID])
	return deps
}

func (m *Manager) CheckMySQLRootAuth() (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var s *ServiceInstance
	for id, inst := range m.services {
		if strings.HasPrefix(id, "mysql@") {
			s = inst
			break
		}
	}
	if s == nil {
		return false, fmt.Errorf("MySQL 服务未注册")
	}

	if !s.Info.Installed {
		return false, fmt.Errorf("MySQL 未安装")
	}

	installDir := m.cfg.Get().InstallDir
	mysqlDir := filepath.Join(installDir, "programs", "mysql", s.Info.Version)
	mysqlBin := filepath.Join(mysqlDir, "bin", "mysql.exe")

	if _, err := os.Stat(mysqlBin); err != nil {
		return false, fmt.Errorf("mysql 客户端未找到")
	}

	port := s.Info.Port
	if port == 0 {
		port = 3306
	}

	pid := process.FindPIDByPort(port)
	if pid <= 0 {
		return false, fmt.Errorf("MySQL 未运行，请先启动")
	}

	args := []string{
		fmt.Sprintf("--port=%d", port),
		"-u", "root",
		"-e", "SELECT 1;",
	}

	cmd := exec.Command(mysqlBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		output := string(out)
		if strings.Contains(output, "Access denied") {
			return true, nil
		}
		return false, fmt.Errorf("检查失败: %s", output)
	}
	return false, nil
}

func (m *Manager) CheckVersionInstalled(serviceType, version string) bool {
	installDir := m.cfg.Get().InstallDir
	if installDir == "" {
		return false
	}
	return dirExists(filepath.Join(installDir, "programs", serviceType, version))
}

func (m *Manager) UninstallVersion(serviceType, version string) error {
	m.mu.Lock()
	installDir := m.cfg.Get().InstallDir
	if installDir == "" {
		m.mu.Unlock()
		return nil
	}

	m.mu.Unlock()

	entryID := serviceType + "@" + version
	compensator := tx.NewCompensator()
	txn := tx.NewTransaction("uninstall-" + entryID)

	binPath := filepath.Join(installDir, "programs", serviceType, version)
	backupPath := binPath + ".backup"

	txn.
		Step("stop-service", func(ctx context.Context) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			if s, ok := m.services[entryID]; ok {
				if s.Info.PID > 0 {
					if s.stopped != nil {
						close(s.stopped)
						s.stopped = nil
					}
					return m.procMgr.Stop(s.Info.PID)
				}
			}
			return nil
		}, compensator.Noop()).
		Step("backup-before-delete", func(ctx context.Context) error {
			if !dirExists(binPath) {
				return nil
			}
			return copyDir(binPath, backupPath)
		}, compensator.Noop()).
		Step("update-service-state", func(ctx context.Context) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			if s, ok := m.services[entryID]; ok {
				s.Info.Installed = false
				s.Info.Status = core.StatusStopped
				s.Info.PID = 0
				s.process = nil
			}
			return nil
		}, compensator.Noop()).
		Step("delete-install-dir", func(ctx context.Context) error {
			if dirExists(binPath) {
				return removeAll(serviceType, binPath)
			}
			return nil
		}, func(ctx context.Context) error {
			if dirExists(backupPath) {
				return copyDir(backupPath, binPath)
			}
			return nil
		})

	if err := txn.Commit(); err != nil {
		if dirExists(backupPath) {
			copyDir(backupPath, binPath)
		}
		return fmt.Errorf("卸载事务失败: %w", err)
	}

	os.RemoveAll(backupPath)
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (m *Manager) GetInstalledVersions(serviceType string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	installDir := m.cfg.Get().InstallDir
	if installDir == "" {
		return nil
	}
	svcDir := filepath.Join(installDir, "programs", serviceType)
	entries, err := os.ReadDir(svcDir)
	if err != nil {
		return nil
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() && dirExists(filepath.Join(svcDir, e.Name())) {
			versions = append(versions, e.Name())
		}
	}
	return versions
}

func (m *Manager) GetService(id string) *ServiceInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.services[id]
}

func serviceTypeFromPkg(pt registry.PackageType) core.ServiceType {
	switch pt {
	case registry.PkgNginx:
		return core.TypeNginx
	case registry.PkgApache:
		return core.TypeApache
	case registry.PkgMySQL:
		return core.TypeMySQL
	case registry.PkgPostgreSQL:
		return core.TypePostgreSQL
	case registry.PkgMongoDB:
		return core.TypeMongoDB
	case registry.PkgRedis:
		return core.TypeRedis
	case registry.PkgMemcached:
		return core.TypeMemcached
	case registry.PkgPHP:
		return core.TypePHP
	case registry.PkgNode:
		return core.TypeNode
	case registry.PkgPython:
		return core.TypePython
	case registry.PkgComposer:
		return core.TypeComposer
	default:
		return ""
	}
}

func serviceCategoryFromPkg(c registry.Category) string {
	switch c {
	case registry.CatWebServer:
		return core.CategoryWebServer
	case registry.CatDatabase:
		return core.CategoryDatabase
	case registry.CatRuntime:
		return "runtime"
	default:
		return core.CategoryService
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var serviceExecutables = map[string][]string{
	"nginx":  {"nginx.exe"},
	"mysql":  {"mysqld.exe"},
	"redis":  {"redis-server.exe"},
	"node":   {"node.exe"},
	"python": {"python.exe"},
}

func removeAll(serviceType, path string) error {
	if exes, ok := serviceExecutables[serviceType]; ok {
		for _, exe := range exes {
			cmd := exec.Command("taskkill", "/f", "/im", exe)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("终止进程 %s 失败: %w", exe, err)
			}
		}
	}

	c := exec.Command("cmd", "/c", "attrib", "-r", "-s", "-h", filepath.Join(path, "*"), "/s")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := c.Run(); err != nil {
		return fmt.Errorf("移除文件属性失败: %w", err)
	}

	rd := exec.Command("cmd", "/c", "rd", "/s", "/q", path)
	rd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return rd.Run()
}
