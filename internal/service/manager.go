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
	"github.com/xiaodu/DevStack/internal/orchestrator"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/provider"
	"github.com/xiaodu/DevStack/internal/reactive"
	"github.com/xiaodu/DevStack/internal/registry"
	"github.com/xiaodu/DevStack/internal/resource"
	"github.com/xiaodu/DevStack/internal/tx"
)

type Manager struct {
	mu         sync.RWMutex
	cfg        *config.Config
	procMgr    *process.Manager
	resMgr     *resource.Manager
	services   map[string]*ServiceInstance
	dependents map[string][]string
	reactive   *reactive.Engine
	orch       *orchestrator.Engine // 服务编排引擎（Windows Job Object）

	syncStop chan struct{} // 周期状态同步的停止信号（nil 表示未启动）
	syncOnce sync.Once
}

type ServiceInstance struct {
	Info         core.ServiceInfo
	process      *process.ProcessHandle
	stopped      chan struct{}
	stoppedFlag  bool // true when user explicitly called Stop — watcher must not trigger restart
	restartCount int
}

func NewManager(cfg *config.Config, procMgr *process.Manager, resMgr *resource.Manager, reactiveEng *reactive.Engine) *Manager {
	// 创建服务编排引擎
	orch, err := orchestrator.NewEngine(
		orchestrator.WithLogCallback(func(msg string) {
			// 日志可以通过 reactive 引擎传递到前端
		}),
		orchestrator.WithStartFunc(func(name string) (int, error) {
			// 启动函数：由 service.Manager 提供，调用 provider 启动服务
			// 编排引擎调用此函数来实际启动服务
			return 0, fmt.Errorf("启动函数未设置: %s", name)
		}),
		orchestrator.WithStopFunc(func(name string) error {
			// 停止函数：由 service.Manager 提供，调用 provider 停止服务
			// 编排引擎调用此函数来实际停止服务
			return fmt.Errorf("停止函数未设置: %s", name)
		}),
	)
	if err != nil {
		// 编排引擎创建失败，记录错误但不影响服务管理器初始化
		// 服务管理器将回退到 provider 模式
		orch = nil
	}

	m := &Manager{
		cfg:        cfg,
		procMgr:    procMgr,
		resMgr:     resMgr,
		services:   make(map[string]*ServiceInstance),
		dependents: make(map[string][]string),
		reactive:   reactiveEng,
		orch:       orch,
	}

	// 设置编排引擎的启动和停止回调
	if orch != nil {
		orch.SetStartFunc(func(name string) (int, error) {
			return m.startServiceByName(name)
		})
		orch.SetStopFunc(func(name string) error {
			return m.stopServiceByName(name)
		})
	}

	m.registerDefaults()
	return m
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
			// 空目录/半解压残留不算已安装：要求主可执行文件存在。
			if !versionInstalled(installDir, svcType, version) {
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
		if pid <= 0 {
			continue
		}
		// 端口占用者必须是本 kind/version 安装目录内的进程才算本实例在运行：
		// 多个版本共享端口（如 nginx 1.20/1.24/1.26 共享 80）时只有真正占用者标记为运行中，
		// 端口被其它程序占用时也不应把本服务误标为运行中。
		if exePathMatchesPID(pid, installDir, s.Info.Kind, s.Info.Version) {
			s.Info.Status = core.StatusRunning
			s.Info.PID = pid
		}
	}
}

// LoadProcfile 加载 Procfile 到编排引擎
//
// 从指定路径加载 Procfile，将服务定义注册到编排引擎。
// 如果编排引擎未初始化，此方法静默返回 nil。
func (m *Manager) LoadProcfile(path string) error {
	if m.orch == nil {
		return nil
	}
	return m.orch.LoadProcfile(path)
}

// GetOrchestrator 返回编排引擎实例（用于外部调用）
func (m *Manager) GetOrchestrator() *orchestrator.Engine {
	return m.orch
}

// findServiceByName 根据服务名称查找服务实例
//
// 编排引擎使用服务名称（如 "Nginx"），而 service.Manager 使用 ID（如 "nginx@1.26.0"）。
// 此方法通过名称匹配查找对应的服务实例。
//
// 精确 ID 必须优先：注册表按 programs\<kind>\<version> 扫描出多份同 kind 实例
// （如 nginx@1.20.2 与 nginx@1.26.0），若按 kind 模糊匹配，Go map 的随机遍历
// 会导致每次激活随机启动/停止任意版本——这正是"环境写的是 1.26.0 却拉起了
// 1.20.2"的根源。
func (m *Manager) findServiceByName(name string) *ServiceInstance {
	if s, ok := m.services[name]; ok {
		return s
	}
	lowerName := strings.ToLower(name)
	for _, s := range m.services {
		if strings.ToLower(s.Info.Name) == lowerName || strings.ToLower(s.Info.Kind) == lowerName {
			return s
		}
	}
	return nil
}

// startServiceByName 由 orchestrator 调用，实际启动服务并返回 PID
//
// 编排引擎通过 StartFunc 回调调用此方法。
// 方法内部调用 provider 启动服务，返回启动后的 PID。
func (m *Manager) startServiceByName(name string) (int, error) {
	m.mu.Lock()
	s := m.findServiceByName(name)
	if s == nil {
		m.mu.Unlock()
		return 0, fmt.Errorf("服务 %s 不存在", name)
	}

	kind := kindFromID(s.Info.ID)
	version := s.Info.Version
	port := s.Info.Port
	m.mu.Unlock()

	// 使用 Provider 启动服务
	p := provider.Get(kind)
	if p == nil {
		return 0, fmt.Errorf("不支持的服务类型: %s", kind)
	}

	config := map[string]interface{}{
		"version": version,
		"port":    port,
	}
	entity, err := p.NewInstance(context.Background(), config)
	if err != nil {
		return 0, err
	}

	if err := entity.Start(context.Background()); err != nil {
		return 0, err
	}

	// 获取启动后的 PID
	pid := process.FindPIDByPort(port)
	return pid, nil
}

// stopServiceByName 由 orchestrator 调用，实际停止服务
//
// 编排引擎通过 StopFunc 回调调用此方法。
// 方法内部调用 provider 停止服务。
func (m *Manager) stopServiceByName(name string) error {
	m.mu.Lock()
	s := m.findServiceByName(name)
	if s == nil {
		m.mu.Unlock()
		return fmt.Errorf("服务 %s 不存在", name)
	}

	kind := kindFromID(s.Info.ID)
	version := s.Info.Version
	port := s.Info.Port
	m.mu.Unlock()

	// 使用 Provider 停止服务
	p := provider.Get(kind)
	if p == nil {
		return fmt.Errorf("不支持的服务类型: %s", kind)
	}

	config := map[string]interface{}{
		"version": version,
		"port":    port,
	}
	entity, err := p.NewInstance(context.Background(), config)
	if err != nil {
		return err
	}

	return entity.Stop(context.Background())
}

var serviceOrder = []string{"nginx", "apache", "mysql", "postgresql", "mongodb", "redis", "memcached", "node", "python", "composer"}

func (m *Manager) matchConfigHandler(id string) ConfigHandler {
	kind := kindFromID(id)
	if h, ok := configHandlers[kind]; ok {
		return h
	}
	return nil
}

func (m *Manager) syncServiceState(s *ServiceInstance, portPIDMap map[int]int) core.ServiceInfo {
	info := s.Info
	installDir := m.cfg.Get().InstallDir

	// ownedPID 返回本服务端口上属于本 kind/version 的进程 PID；端口被其它程序占用时返回 0。
	ownedPID := func() int {
		if info.Port <= 0 {
			return 0
		}
		pid, ok := portPIDMap[info.Port]
		if !ok || pid <= 0 {
			return 0
		}
		if exePathMatchesPID(pid, installDir, info.Kind, info.Version) {
			return pid
		}
		return 0
	}

	switch info.Status {
	case core.StatusStarting, core.StatusStopping:
		// Keep transitional states as-is

	case core.StatusRunning:
		stillRunning := false
		if s.process != nil && s.process.PID > 0 && m.procMgr.IsRunning(s.process.PID) {
			stillRunning = true
		}
		if !stillRunning {
			if pid := ownedPID(); pid > 0 {
				stillRunning = true
				info.PID = pid
			}
		}
		if !stillRunning {
			info.Status = core.StatusStopped
			info.PID = 0
		}

	case core.StatusStopped:
		if pid := ownedPID(); pid > 0 {
			info.Status = core.StatusRunning
			info.PID = pid
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
		if !oki {
			oi = len(serviceOrder)
		}
		if !okj {
			oj = len(serviceOrder)
		}
		if oi != oj {
			return oi < oj
		}
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
	if s.Info.Status == core.StatusRunning || s.Info.Status == core.StatusStarting {
		m.mu.Unlock()
		return nil
	}

	kind := kindFromID(id)
	version := s.Info.Version
	port := s.Info.Port
	s.Info.Status = core.StatusStarting
	s.Info.Error = ""
	m.mu.Unlock()

	m.publishServiceEvent(reactive.EventServiceStarting, id, nil)

	// 使用编排引擎启动（如果可用）
	if m.orch != nil {
		// 确保服务已在编排引擎中注册。
		// 必须用完整 ID（kind@version）注册与启动：按裸 kind 启动会让
		// startServiceByName 走模糊匹配，随机启动其它已安装版本。
		if !m.orch.HasService(id) {
			m.orch.RegisterService(id, "", nil, nil, "", 5)
		}
		// 同步端口：编排引擎的就绪探活/端口冲突检测依赖它。
		// 不传的话 Command/Args 为空提取不到端口，500ms sleep 即谎报就绪。
		if port > 0 {
			m.orch.SetServicePort(id, port)
		}

		// 使用编排引擎启动服务（依赖检查、端口冲突检测、健康等待）
		ctx := context.Background()
		if err := m.orch.Start(ctx, id); err != nil {
			// 编排引擎启动失败，更新状态
			m.mu.Lock()
			if si, ok := m.services[id]; ok {
				si.Info.Status = core.StatusStopped
				si.Info.Error = err.Error()
			}
			m.mu.Unlock()
			m.publishServiceEvent(reactive.EventServiceError, id, map[string]interface{}{
				"error": err.Error(),
			})
			return err
		}

		// 获取启动后的 PID
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
		s.Info.Error = ""
		s.stoppedFlag = false
		if proc != nil && proc.PID > 0 {
			s.Info.PID = proc.PID
		}
		s.process = proc
		s.stopped = make(chan struct{})
		s.restartCount = 0

		stoppedChan := s.stopped
		m.mu.Unlock()

		m.publishServiceEvent(reactive.EventServiceStarted, id, nil)

		if proc != nil {
			go m.watchProcess(id, stoppedChan)
		}
		return nil
	}

	// 回退到 provider 模式（编排引擎不可用时）
	p := provider.Get(kind)
	if p == nil {
		m.mu.Lock()
		if si, ok := m.services[id]; ok {
			si.Info.Status = core.StatusStopped
		}
		m.mu.Unlock()
		m.publishServiceEvent(reactive.EventServiceError, id, map[string]interface{}{
			"error": fmt.Sprintf("不支持的服务类型: %s", kind),
		})
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
			si.Info.Error = err.Error()
		}
		m.mu.Unlock()
		m.publishServiceEvent(reactive.EventServiceError, id, map[string]interface{}{
			"error": err.Error(),
		})
		return err
	}
	if err := entity.Start(context.Background()); err != nil {
		m.mu.Lock()
		if si, ok := m.services[id]; ok {
			si.Info.Status = core.StatusStopped
			si.Info.Error = err.Error()
		}
		m.mu.Unlock()
		m.publishServiceEvent(reactive.EventServiceError, id, map[string]interface{}{
			"error": err.Error(),
		})
		return err
	}
	pid := process.FindPIDByPort(port)
	proc := &process.ProcessHandle{PID: pid, Started: time.Now()}
	if pid <= 0 {
		proc = nil
	}

	// 将进程加入 Job Object（如果编排引擎可用）
	if m.orch != nil && pid > 0 {
		m.orch.AssignPID(kind, pid)
	}

	m.mu.Lock()
	s = m.services[id]
	if s == nil {
		m.mu.Unlock()
		return core.ErrServiceNotFound
	}
	s.Info.Status = core.StatusRunning
	s.Info.Error = ""
	s.stoppedFlag = false
	if proc != nil && proc.PID > 0 {
		s.Info.PID = proc.PID
	}
	s.process = proc
	s.stopped = make(chan struct{})
	s.restartCount = 0

	stoppedChan := s.stopped
	m.mu.Unlock()

	m.publishServiceEvent(reactive.EventServiceStarted, id, nil)

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
	installDir := m.cfg.Get().InstallDir

	s.Info.Status = core.StatusStopping
	m.mu.Unlock()

	m.publishServiceEvent(reactive.EventServiceStopping, id, nil)

	// 使用编排引擎停止（如果可用）
	if m.orch != nil {
		ctx := context.Background()
		// 与 Start 对应：必须用完整 ID 停止，否则可能停掉同 kind 的其它版本
		stopErr := m.orch.Stop(ctx, id)

		// 从 Job Object 中释放进程
		m.mu.RLock()
		var pid int
		if si, ok := m.services[id]; ok && si.process != nil {
			pid = si.process.PID
		}
		m.mu.RUnlock()
		if pid > 0 {
			m.orch.ReleasePID(id, pid)
		}

		// 复核真实状态：端口仍被本实例进程监听说明没停掉，绝不向上层谎报"已停止"，
		// 否则 UI 显示已停止而进程还在跑，下次 Start 还会被幂等跳过
		if occ := process.FindPIDByPort(port); occ > 0 && exePathMatchesPID(occ, installDir, kind, version) {
			msg := fmt.Sprintf("停止失败: 进程 PID %d 仍在监听端口 %d", occ, port)
			if stopErr != nil {
				msg = "停止失败: " + stopErr.Error()
			}
			m.mu.Lock()
			if si, ok := m.services[id]; ok {
				si.Info.Status = core.StatusError
				si.Info.Error = msg
			}
			m.mu.Unlock()
			m.publishServiceEvent(reactive.EventServiceError, id, map[string]interface{}{
				"error": msg,
			})
			return fmt.Errorf("%s", msg)
		}
		// stopErr != nil 但进程确实退出了：按已停止处理（orch 超时误报等场景），不阻塞用户

		// 更新状态
		m.mu.Lock()
		if si, ok := m.services[id]; ok {
			si.process = nil
			si.Info.Status = core.StatusStopped
			si.Info.PID = 0
			si.Info.Error = ""
		}
		m.mu.Unlock()

		m.publishServiceEvent(reactive.EventServiceStopped, id, nil)
		return nil
	}

	// 回退到 provider 模式（编排引擎不可用时）
	var stopErr error
	p := provider.Get(kind)
	if p != nil {
		config := map[string]interface{}{
			"version": version,
			"port":    port,
		}
		entity, err := p.NewInstance(context.Background(), config)
		if err != nil {
			m.mu.Lock()
			if si, ok := m.services[id]; ok {
				si.Info.Status = core.StatusStopped
				si.Info.Error = err.Error()
			}
			m.mu.Unlock()
			m.publishServiceEvent(reactive.EventServiceError, id, map[string]interface{}{
				"error": err.Error(),
			})
			return err
		}
		stopErr = entity.Stop(context.Background())
	} else {
		// 无 provider 注册的服务：按端口反查本实例进程并终止
		pid := process.FindPIDByPort(port)
		if pid > 0 && exePathMatchesPID(pid, installDir, kind, version) {
			stopErr = m.procMgr.Stop(pid)
		}
	}

	// 从 Job Object 中释放进程
	if m.orch != nil {
		m.mu.RLock()
		var pid int
		if si, ok := m.services[id]; ok && si.process != nil {
			pid = si.process.PID
		}
		m.mu.RUnlock()
		if pid > 0 {
			m.orch.ReleasePID(id, pid)
		}
	}

	// 更新最终状态
	m.mu.Lock()
	if si, ok := m.services[id]; ok {
		if stopErr != nil {
			si.Info.Status = core.StatusError
			si.Info.Error = stopErr.Error()
		} else {
			si.process = nil
			si.Info.Status = core.StatusStopped
			si.Info.PID = 0
			si.Info.Error = ""
		}
	}
	m.mu.Unlock()

	if stopErr != nil {
		m.publishServiceEvent(reactive.EventServiceError, id, map[string]interface{}{
			"error": stopErr.Error(),
		})
		return stopErr
	}

	m.publishServiceEvent(reactive.EventServiceStopped, id, nil)
	return nil
}

func (m *Manager) publishServiceEvent(eventType reactive.EventType, serviceID string, extra map[string]interface{}) {
	data := map[string]interface{}{
		"serviceID": serviceID,
	}
	for k, v := range extra {
		data[k] = v
	}
	m.reactive.Publish(reactive.NewEvent(eventType, serviceID, data))
}

// StartBackgroundSync 启动周期状态同步（幂等）：每 5 秒按端口+进程归属复核全部服务状态，
// 与 s.Info 不一致时回写并向前端发布事件。
// 之前状态只在用户操作或前端拉取 List 时被动计算，且 List 的修正结果不回写——
// 进程被外部杀死后（如用户 taskkill），存储状态永远停留在"运行中"。
// 周期同步同时是 reactive 事件丢失时的兜底真相源。
func (m *Manager) StartBackgroundSync() {
	m.syncOnce.Do(func() {
		m.syncStop = make(chan struct{})
		go m.backgroundSyncLoop()
	})
}

func (m *Manager) backgroundSyncLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.syncStop:
			return
		case <-ticker.C:
			m.reconcileStates()
		}
	}
}

// reconcileStates 单次复核：真实状态与存储状态不一致时回写 + 发事件。
func (m *Manager) reconcileStates() {
	// 快照服务列表（避免长时间持锁）
	m.mu.RLock()
	ids := make([]string, 0, len(m.services))
	for id := range m.services {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	if len(ids) == 0 {
		return
	}

	portPIDMap := process.FindAllPIDsByPort()

	type change struct {
		id     string
		info   core.ServiceInfo
		events []struct {
			evt   reactive.EventType
			extra map[string]interface{}
		}
	}
	var changes []change

	m.mu.Lock()
	for _, id := range ids {
		s := m.services[id]
		if s == nil {
			continue
		}
		// 过渡态（启动中/停止中）由对应操作收敛，不抢写
		if s.Info.Status == core.StatusStarting || s.Info.Status == core.StatusStopping {
			continue
		}
		info := m.syncServiceState(s, portPIDMap)
		if info.Status == s.Info.Status && info.PID == s.Info.PID {
			continue
		}
		old := s.Info.Status
		s.Info.Status = info.Status
		s.Info.PID = info.PID
		if info.Status == core.StatusStopped && old == core.StatusRunning {
			// 进程消失：清掉句柄，允许下次 Start 正常走流程
			s.process = nil
			c := change{id: id, info: info}
			c.events = append(c.events, struct {
				evt   reactive.EventType
				extra map[string]interface{}
			}{reactive.EventServiceStopped, map[string]interface{}{"reason": "process-exited"}})
			changes = append(changes, c)
		} else if info.Status == core.StatusRunning && old == core.StatusStopped {
			// 端口上发现本实例进程（外部手动启动等）：认领
			s.process = &process.ProcessHandle{PID: info.PID, Started: time.Now()}
			c := change{id: id, info: info}
			c.events = append(c.events, struct {
				evt   reactive.EventType
				extra map[string]interface{}
			}{reactive.EventServiceStarted, map[string]interface{}{"reason": "adopted"}})
			changes = append(changes, c)
		}
	}
	m.mu.Unlock()

	// 锁外发事件，避免 handler 回调死锁
	for _, c := range changes {
		for _, ev := range c.events {
			m.publishServiceEvent(ev.evt, c.id, ev.extra)
		}
	}
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
			if s2 == nil || s2.stoppedFlag || s2.Info.Status != core.StatusRunning {
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
					// 复检：重检期间服务可能已被用户停止或状态已变化，不再触发自愈
					if s3 == nil || s3.stoppedFlag || s3.Info.Status != core.StatusRunning {
						m.mu.Unlock()
						return
					}
					s3.restartCount++
					rc := s3.restartCount
					s3.process = nil
					s3.Info.PID = 0
					msg := ""
					if rc > maxRestartCount {
						// 超过最大重试次数，停止自动重启并置为错误态
						s3.Info.Status = core.StatusError
						msg = fmt.Sprintf("服务连续崩溃 %d 次，已停止自动重启", maxRestartCount)
						s3.Info.Error = msg
					} else {
						// 移出 Running 态，使重启规则能真正执行 Start
						s3.Info.Status = core.StatusStopped
					}
					m.mu.Unlock()

					m.reactive.Publish(reactive.NewEvent(
						reactive.EventServiceHealthFailed,
						id,
						map[string]interface{}{
							"serviceID":    id,
							"restartCount": rc,
						},
					))
					m.reactive.Publish(reactive.NewEvent(
						reactive.EventServiceCrashed,
						id,
						map[string]interface{}{
							"serviceID":    id,
							"restartCount": rc,
							"maxRetries":   maxRestartCount,
							"error":        msg,
						},
					))
					// 若发生重启，Start 会重置计数并注册新的 watcher；本 watcher 在此退出，避免重复计数
					return
				}
			}
		}
	}
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
	s, ok := m.services[id]
	if !ok {
		m.mu.Unlock()
		return core.ErrServiceNotFound
	}

	if s.stopped != nil {
		close(s.stopped)
		s.stopped = nil
	}

	var pids []int
	if s.process != nil {
		pids = append(pids, s.process.PID)
		pids = append(pids, s.process.ExtraPIDs...)
	}
	kind := kindFromID(id)
	version := s.Info.Version
	port := s.Info.Port
	installDir := m.cfg.Get().InstallDir
	// 从登记表移除并清理依赖记账：已卸载的服务不应继续出现在列表/依赖检查里
	delete(m.services, id)
	delete(m.dependents, id)
	for k, deps := range m.dependents {
		kept := deps[:0]
		for _, d := range deps {
			if d != id {
				kept = append(kept, d)
			}
		}
		m.dependents[k] = kept
	}
	m.mu.Unlock()

	// 接管清理残留进程：上一会话遗留的进程 s.process 为 nil 但仍在运行，
	// 只停登记过的 PID 会让删目录报"删除失败"且用户看不懂原因。
	// 按端口 + 安装目录归属识别本实例进程（不误杀外部程序）
	if port > 0 {
		if occ := process.FindPIDByPort(port); occ > 0 && exePathMatchesPID(occ, installDir, kind, version) {
			found := false
			for _, p := range pids {
				if p == occ {
					found = true
					break
				}
			}
			if !found {
				pids = append(pids, occ)
			}
		}
	}

	// 只停止本实例自有进程（不按镜像名全局清理），等待其退出后再删除安装目录
	for _, pid := range pids {
		if pid > 0 {
			m.procMgr.Stop(pid)
		}
	}
	waitPIDsExited(pids, 3*time.Second)

	binPath := filepath.Join(installDir, "programs", kind, version)
	if dirExists(binPath) {
		if err := removeAll(binPath); err != nil {
			// 卸载失败时恢复登记条目，避免 UI 丢失该服务（可重试）
			m.mu.Lock()
			if _, exists := m.services[id]; !exists {
				m.services[id] = s
				s.Info.Installed = true
			}
			m.mu.Unlock()
			return fmt.Errorf("删除安装目录失败: %w", err)
		}
	}

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
	// 新装服务必须带上默认端口：registerDefaults 的磁盘扫描只在启动时执行一次，
	// 通过「下载安装」新增的服务走不到那里。Port=0 会导致启动判活、
	// watchProcess、端口冲突检测全线失效
	port := 0
	if pkg := registry.Builtin().Get(kind); pkg != nil {
		port = pkg.DefaultPort
	}
	m.services[entryID] = &ServiceInstance{
		Info: core.ServiceInfo{
			ID: entryID, Kind: kind, Name: kindNames[kind],
			Type:    core.ServiceType(kind),
			Version: version, Installed: true, Port: port,
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
		// 调用方只传 kind（如前端 applyServiceConfig('nginx')）时不能按 map 随机序挑版本，
		// 否则多版本共存时配置到不可预期的版本上。确定性选择：运行中 > 默认版本 > 最高版本。
		kind := kindFromID(id)
		for _, cand := range m.services {
			if cand.Info.Kind != kind {
				continue
			}
			if s == nil || cand.Info.Status == core.StatusRunning {
				s = cand
				ok = true
				if s.Info.Status == core.StatusRunning {
					break
				}
				continue
			}
			if s.Info.Status != core.StatusRunning && cand.Info.Version > s.Info.Version {
				s = cand
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
			m.mu.RLock()
			si := m.services[id]
			m.mu.RUnlock()
			if si == nil {
				return core.ErrServiceNotFound
			}
			return handler.Configure(si, m.cfg, m.resMgr)
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
		"--host=127.0.0.1",
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
	return versionInstalled(installDir, core.ServiceType(serviceType), version)
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
			s, ok := m.services[entryID]
			if !ok {
				m.mu.Unlock()
				return nil
			}
	if s.stopped != nil {
		close(s.stopped)
		s.stopped = nil
	}
	s.stoppedFlag = true
			var pids []int
			if s.process != nil {
				pids = append(pids, s.process.PID)
				pids = append(pids, s.process.ExtraPIDs...)
			}
			s.Info.PID = 0
			s.process = nil
			m.mu.Unlock()

			// 只停止本实例自有进程，等待退出后再删除，避免 rd 因文件占用失败
			for _, pid := range pids {
				if pid > 0 {
					m.procMgr.Stop(pid)
				}
			}
			waitPIDsExited(pids, 3*time.Second)
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
				return removeAll(binPath)
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

// versionExeRel 给出每个服务类型安装目录下的主可执行文件相对路径
// （相对 programs/<kind>/<version>/）。目录存在不代表装好了——崩溃残留的
// 半解压目录、手动建的空目录同样在。exe 存在才是"已安装"的可靠判据。
// 路径与各 provider.IsInstalled 保持一致；未列入的类型（apache/pgsql/mongo 等）
// 退化为目录存在性判断，避免用错 exe 名误伤。php 由 runtime 侧管理，不走此表。
var versionExeRel = map[core.ServiceType]string{
	core.TypeNginx: "nginx.exe",
	core.TypeMySQL: filepath.Join("bin", "mysqld.exe"),
	core.TypeRedis: "redis-server.exe",
}

// versionInstalled 判定 programs/<kind>/<version> 是否为完整安装：
// 已知类型要求主可执行文件存在，未知类型退化为目录存在性判断。
func versionInstalled(installDir string, svcType core.ServiceType, version string) bool {
	verDir := filepath.Join(installDir, "programs", string(svcType), version)
	exeRel, ok := versionExeRel[svcType]
	if !ok {
		return dirExists(verDir)
	}
	info, err := os.Stat(filepath.Join(verDir, exeRel))
	return err == nil && !info.IsDir()
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
		if e.IsDir() && versionInstalled(installDir, core.ServiceType(serviceType), e.Name()) {
			versions = append(versions, e.Name())
		}
	}
	return versions
}

func (m *Manager) GetService(id string) *ServiceInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.services[id]
	if !ok {
		return nil
	}
	// 返回值拷贝，避免调用方在锁外读取 Info 等字段时与 Stop/Start 的写竞争
	copy := *s
	return &copy
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

// waitPIDsExited 等待指定进程全部退出；在 timeout 内全部退出时立即返回。
func waitPIDsExited(pids []int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range pids {
			if pid > 0 && process.IsProcessRunning(pid) {
				alive = true
				break
			}
		}
		if !alive {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func removeAll(path string) error {
	c := exec.Command("cmd", "/c", "attrib", "-r", "-s", "-h", filepath.Join(path, "*"), "/s")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := c.Run(); err != nil {
		return fmt.Errorf("移除文件属性失败: %w", err)
	}

	rd := exec.Command("cmd", "/c", "rd", "/s", "/q", path)
	rd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return rd.Run()
}

// Shutdown 关闭服务管理器，清理编排引擎
//
// 在应用退出时调用，确保所有通过编排引擎管理的进程被正确终止。
func (m *Manager) Shutdown(ctx context.Context) error {
	if m.orch != nil {
		return m.orch.Shutdown(ctx)
	}
	return nil
}
