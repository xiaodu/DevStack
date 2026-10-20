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

	// stopNotes 暂存 provider 停止过程中的告警（键为服务 ID）。
	//
	// 为何需要中转：阶梯式停止里"最终确实停了、但过程有异常"（优雅关闭被
	// 认证拒收、实际靠强杀收尾）不能当成 error 返回 —— 上层会把任何非 nil
	// 停止错误写成 StatusError，把"已停"误报成"没停"。而编排路径的
	// stopServiceByName 返回值签名由 orchestrator.StopFunc 固定，无法携带备注，
	// 故在此暂存，由 Stop 取出后以 warning 上浮到 UI。
	stopNotes map[string]string
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
		stopNotes:  make(map[string]string),
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

// SetResourceManager 注入资源管理器。首跑向导之前安装目录为空、资源管理器尚未创建，
// 向导完成后必须补注入，否则端口分配与配置落盘会走 nil（见 app.reinitManagers）。
func (m *Manager) SetResourceManager(rm *resource.Manager) {
	m.mu.Lock()
	m.resMgr = rm
	m.mu.Unlock()
}

// resourceMgr 读取当前资源管理器（受同一把锁保护）。
func (m *Manager) resourceMgr() *resource.Manager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.resMgr
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
	portPIDMap := process.FindAllPIDsByPort()
	for _, s := range m.services {
		pids := ownedPIDsFromMap(portPIDMap, s.Info.Port, installDir, s.Info.Kind, s.Info.Version)
		if len(pids) == 0 {
			continue
		}
		// 端口占用者必须是本 kind/version 安装目录内的进程才算本实例在运行：
		// 多个版本共享端口（如 nginx 1.20/1.24/1.26 共享 80）时只有真正占用者标记为运行中，
		// 端口被其它程序占用时也不应把本服务误标为运行中。
		s.Info.Status = core.StatusRunning
		s.Info.PID = pids[0]
		s.process = processHandleFromPIDs(pids)
		// 冷启动接管：进程在跑就必须同步登记到编排引擎，否则停止时引擎查不到条目
		// （历史表现：界面显示运行中，点停止报"服务 xxx 不存在"且进程一个都没杀）。
		m.adoptOrchestrator(s.Info.ID, s.Info.Port, pids)
	}
}

// adoptOrchestrator 把"端口上实际在跑的一批进程"登记到编排引擎（幂等，引擎不可用时静默跳过）。
func (m *Manager) adoptOrchestrator(id string, port int, pids []int) {
	if m.orch == nil || len(pids) == 0 {
		return
	}
	if err := m.orch.Adopt(id, port, pids); err != nil {
		// 接管失败不影响判活，只影响 Job Object 归属；不阻断启动流程
		m.reactive.Publish(reactive.NewEvent(reactive.EventServiceHealthFailed, id, map[string]interface{}{
			"serviceID": id,
			"reason":    err.Error(),
		}))
	}
}

// livePIDs 返回本实例（kind@version 安装目录）当前在端口上运行的全部进程 PID。
//
// fresh=true 时先作废端口缓存，用于强杀之后的复核：netstat 结果有 2s TTL，
// 不作废就会读到停止动作发生前的快照，把已经停成功的操作误判成"仍在监听"。
func (m *Manager) livePIDs(kind, version string, port int, fresh bool) []int {
	if port <= 0 {
		return nil
	}
	if fresh {
		process.InvalidatePortCache()
	}
	return process.FindPIDsByPortOwned(port, instanceDir(m.cfg.Get().InstallDir, kind, version))
}

// processHandleFromPIDs 把本实例当前占端口的全部进程（master + workers）收进句柄。
// 只记 pids[0] 不够：nginx reload 会整批更换 worker，单 PID 很快指向已退出进程。
func processHandleFromPIDs(pids []int) *process.ProcessHandle {
	if len(pids) == 0 {
		return nil
	}
	extra := make([]int, len(pids)-1)
	copy(extra, pids[1:])
	return &process.ProcessHandle{PID: pids[0], ExtraPIDs: extra, Started: time.Now()}
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

	// 获取启动后的 PID：必须按"本 kind/version 安装目录"过滤，
	// 直接取端口上的任意监听者会在多版本共享端口（nginx 1.20/1.24/1.26 同占 80）时
	// 把别的版本的进程 PID 记到本实例头上。
	pids := m.livePIDs(kind, version, port, false)
	if len(pids) == 0 {
		return 0, nil
	}
	return pids[0], nil
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

	if err := entity.Stop(context.Background()); err != nil {
		return err
	}
	// 已停但过程有异常：暂存告警，由 Stop 的编排分支取出后上浮（见 Manager.stopNotes）。
	if tn, ok := entity.(interface{ StopNote() string }); ok {
		if note := tn.StopNote(); note != "" {
			m.mu.Lock()
			m.stopNotes[s.Info.ID] = note
			m.mu.Unlock()
		}
	}
	return nil
}

// takeStopNote 取出并清除某服务上次停止留下的告警（无则空串）。
func (m *Manager) takeStopNote(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	note := m.stopNotes[id]
	delete(m.stopNotes, id)
	return note
}

var serviceOrder = []string{"nginx", "apache", "mysql", "postgresql", "mongodb", "redis", "memcached", "node", "python", "composer"}

func (m *Manager) matchConfigHandler(id string) ConfigHandler {
	kind := kindFromID(id)
	if h, ok := configHandlers[kind]; ok {
		return h
	}
	return nil
}

func (m *Manager) syncServiceState(s *ServiceInstance, portPIDMap map[int][]int) core.ServiceInfo {
	info := s.Info
	installDir := m.cfg.Get().InstallDir

	// ownedPIDs 返回本服务端口上属于本 kind/version 实例的全部进程 PID。
	// 端口被其它程序或其它版本占用时返回空，既不会误判在运行，也不会误杀外部进程。
	ownedPIDs := func() []int {
		return ownedPIDsFromMap(portPIDMap, info.Port, installDir, info.Kind, info.Version)
	}

	switch info.Status {
	case core.StatusStarting, core.StatusStopping:
		// Keep transitional states as-is

	case core.StatusRunning:
		stillRunning := false
		if s.process != nil && s.process.PID > 0 && m.procMgr.IsRunning(s.process.PID) {
			stillRunning = true
		}
		pids := ownedPIDs()
		if !stillRunning && len(pids) > 0 {
			stillRunning = true
			info.PID = pids[0]
		}
		if !stillRunning {
			info.Status = core.StatusStopped
			info.PID = 0
		}

	case core.StatusStopped:
		if pids := ownedPIDs(); len(pids) > 0 {
			info.Status = core.StatusRunning
			info.PID = pids[0]
		}

	case core.StatusError:
		// 错误态必须可自愈：之前 switch 里没有这一分支，服务一旦落 Error
		// 就永远显示"错误"（哪怕进程早就停干净了），用户只能重启整个 DevStack。
		// 这里回到唯一真相——端口上到底还有没有本实例的进程。
		if pids := ownedPIDs(); len(pids) > 0 {
			info.Status = core.StatusRunning
			info.PID = pids[0]
			info.Error = ""
			if s.process == nil || s.process.PID <= 0 {
				s.process = processHandleFromPIDs(pids)
			}
		} else {
			info.Status = core.StatusStopped
			info.PID = 0
			info.Error = ""
			s.process = nil
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

	return result
}

// resolvePortConflicts 的旧实现已删除。
//
// 旧逻辑建立在"一个端口只有一个监听 PID"的前提上：同端口多版本时随机挑一个
// （匹配不到就取排序首位）当作唯一 owner，把其它版本一律压成"已停止"。
// 现在 syncServiceState 按"该 kind@version 安装目录实际拥有哪些 PID"判定归属，
// 归属是精确的，不再需要事后压制；两个版本真的同时占同一端口属于真实的端口冲突，
// 应该如实暴露出来，而不是被随机压掉一个制造"没在运行"的假象。

// instanceDir 返回某服务实例的安装根目录（<install>\programs\<kind>\<version>），
// 它是"哪些进程属于这个实例"的唯一归属依据。
func instanceDir(installDir, kind, version string) string {
	return filepath.Join(installDir, "programs", kind, version)
}

// ownedPIDsFromMap 从端口→PID 集合里取出属于本实例（kind@version 目录）的全部 PID。
//
// 必须返回集合：Windows 上 nginx/redis 等的 master 与每个 worker 都会出现在同一端口的
// netstat 行里，只取一个 PID 会导致"杀一个 worker、master 重生一个"的死循环。
func ownedPIDsFromMap(portPIDMap map[int][]int, port int, installDir, kind, version string) []int {
	if port <= 0 {
		return nil
	}
	dir := strings.ToLower(instanceDir(installDir, kind, version))
	var out []int
	for _, pid := range portPIDMap[port] {
		exe := strings.ToLower(process.GetExecutablePath(pid))
		if exe != "" && strings.Contains(exe, dir) {
			out = append(out, pid)
		}
	}
	return out
}

// exePathMatchesPID 判断端口监听进程的 exe 路径是否落在 installDir/programs/<kind>/<version> 目录下。
func exePathMatchesPID(pid int, installDir, kind, version string) bool {
	exe := process.GetExecutablePath(pid)
	if exe == "" {
		return false
	}
	installPath := strings.ToLower(instanceDir(installDir, kind, version))
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
	switch s.Info.Status {
	case core.StatusRunning:
		m.mu.Unlock()
		return nil
	case core.StatusStarting:
		// 上一次启动还在进行中：不能回"成功"（进程其实没起来），也不能再拉起一份
		m.mu.Unlock()
		return core.ErrStartInProgress
	}

	kind := kindFromID(id)
	version := s.Info.Version
	port := s.Info.Port
	s.Info.Status = core.StatusStarting
	s.Info.Error = ""
	m.mu.Unlock()

	m.publishServiceEvent(reactive.EventServiceStarting, id, nil)

	// 端口被同 kind 的其它版本占着时，先显式停掉那个版本再启动：
	// provider 侧已不再跨版本静默强杀（那会让被杀版本在 UI 上继续显示运行中），
	// 这里走正常 Stop 流程，会广播停止事件，界面状态与真实进程保持同步。
	if port > 0 {
		if holder := m.instanceOnPort(kind, port, id); holder != "" {
			if err := m.Stop(holder); err != nil {
				return fmt.Errorf("启动 %s 需要先停止占用端口 %d 的 %s：%w", id, port, holder, err)
			}
		}
	}

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

		// 获取启动后的 PID 集合（master + 全部 worker），并同步登记到编排引擎。
		// 只记一个 PID 会让后续停止/复核漏掉其余进程，Job Object 也管不全。
		pids := m.livePIDs(kind, version, port, false)
		proc := processHandleFromPIDs(pids)
		m.adoptOrchestrator(id, port, pids)

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
		oldProc := s.process
		s.process = proc
		s.stopped = make(chan struct{})
		// 崩溃计数只在上一轮真的稳定运行过之后才清零，见 decayRestartCount
		s.restartCount = decayRestartCount(s.restartCount, oldProc)

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
	pids := m.livePIDs(kind, version, port, false)
	proc := processHandleFromPIDs(pids)

	// 将本实例的全部进程纳入 Job Object 管理。
	// 键必须是完整 ID（kind@version）：之前用裸 kind 登记成 "nginx"，
	// 而停止/释放用 "nginx@1.24.0"，两个键空间对不上时 Job Object 形同虚设。
	m.adoptOrchestrator(id, port, pids)

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
	oldProc := s.process
	s.process = proc
	s.stopped = make(chan struct{})
	s.restartCount = decayRestartCount(s.restartCount, oldProc)

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

	if s.Info.Status == core.StatusStarting {
		// 启动进行中不允许穿插停止：两条流程会互相覆盖状态（Start 尾段会把状态写回
		// Running，Stop 的复核又可能误判），此前无防护。
		m.mu.Unlock()
		return core.ErrStopBlocked
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

		// 复核真实状态：端口上仍有本实例进程就说明没停干净。
		// 先做最终强制终止（provider 可能漏杀 worker/孤儿进程树），再复核；
		// 仍不干净才如实报错，绝不谎报"已停止"（否则 UI 显示已停而进程还在，下次 Start 被幂等跳过）。
		//
		// 两个必须按集合/真实扫描处理的点：
		// ① 判据是"本实例在该端口上的全部 PID"而不是随机一个 worker —— 只杀一个，
		//    master 会立刻重生 worker 抢回端口，永远复核不干净；
		// ② 每轮复核前作废端口缓存 —— netstat 结果有 2s TTL，
		//    否则轮询全程读到停止前的陈旧快照，把已经停成功的动作误报成停止失败。
		remaining := m.livePIDs(kind, version, port, true)
		for round := 0; len(remaining) > 0 && round < 5; round++ {
			for _, pid := range remaining {
				_ = m.procMgr.Stop(pid) // taskkill /F /T
			}
			time.Sleep(400 * time.Millisecond)
			remaining = m.livePIDs(kind, version, port, true)
		}
		if len(remaining) > 0 {
			msg := fmt.Sprintf("停止失败: 端口 %d 上仍有本服务进程 (PID %v) 未退出，可能需要更高权限或手动结束", port, remaining)
			if stopErr != nil {
				msg += "：" + stopErr.Error()
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
		// stopErr != nil 但进程确实全部退出了：端口/进程状态按"已停止"落账是诚实的，
		// 但优雅关闭没成功这件事不能默默吞掉——记在 Error 上并回给调用方，
		// 让界面既显示"已停止"又显示"停止过程有异常"。
		if stopErr != nil {
			warn := "已停止，但优雅关闭未完成：" + stopErr.Error()
			m.mu.Lock()
			if si, ok := m.services[id]; ok {
				si.process = nil
				si.Info.Status = core.StatusStopped
				si.Info.PID = 0
				si.Info.Error = warn
			}
			m.mu.Unlock()
			m.publishServiceEvent(reactive.EventServiceStopped, id, map[string]interface{}{"warning": warn})
			return fmt.Errorf("%s", warn)
		}

		// 更新状态
		// stopNote：进程确实停了，但停止过程有异常（如靠强杀收尾）。它不是 error，
		// 状态按 Stopped 诚实落账，同时写进 Error 并用事件的 warning 载荷推给 UI。
		note := m.takeStopNote(id)
		m.mu.Lock()
		if si, ok := m.services[id]; ok {
			si.process = nil
			si.Info.Status = core.StatusStopped
			si.Info.PID = 0
			si.Info.Error = note
		}
		m.mu.Unlock()

		if note != "" {
			m.publishServiceEvent(reactive.EventServiceStopped, id, map[string]interface{}{"warning": note})
		} else {
			m.publishServiceEvent(reactive.EventServiceStopped, id, nil)
		}
		return nil
	}

	// 回退到 provider 模式（编排引擎不可用时）
	var stopErr error
	var stopNote string
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
		// 阶梯式停止里"最终确实停了、但过程有异常"（如优雅关闭被认证拒收、
		// 实际靠强杀收尾）的 provider **不返回 error** —— 下面任何一个非 nil
		// stopErr 都会被写成 StatusError，把"已停"误报成"没停"。
		// 异常改由 StopNote 上浮，语义与上面编排路径的 warn 分支完全一致：
		// 状态诚实（Stopped）、风险可见（Error 字段 + 事件 warning 载荷）。
		if tn, ok := entity.(interface{ StopNote() string }); ok {
			stopNote = tn.StopNote()
		}
	} else {
		// 无 provider 注册的服务：按端口 + 安装目录归属找出本实例的全部进程并终止。
		// 只杀一个 PID 对 master/worker 结构无效（master 会重生 worker）。
		pids := m.livePIDs(kind, version, port, false)
		for _, pid := range pids {
			if err := m.procMgr.Stop(pid); err != nil {
				stopErr = err
			}
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
			si.Info.Error = stopNote
		}
	}
	m.mu.Unlock()

	if stopErr != nil {
		m.publishServiceEvent(reactive.EventServiceError, id, map[string]interface{}{
			"error": stopErr.Error(),
		})
		return stopErr
	}

	if stopNote != "" {
		m.publishServiceEvent(reactive.EventServiceStopped, id, map[string]interface{}{"warning": stopNote})
		return nil
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
	type adoptTask struct {
		id   string
		port int
		pids []int
	}
	var adopts []adoptTask

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
		changed := info.Status != s.Info.Status || info.PID != s.Info.PID
		old := s.Info.Status
		s.Info.Status = info.Status
		s.Info.PID = info.PID
		s.Info.Error = info.Error

		// 运行中的实例必须始终在编排引擎里有登记（幂等 Adopt）：
		// 登记是 Start 的副作用，冷启动接管、reinit 清表、外部启动都会让"进程在跑但没登记"，
		// 此时 Stop 会以记账缺失为由拒绝执行 —— 用户看到的就是"运行中却停不掉"。
		// 真正的接管动作放到锁外做（涉及进程枚举与 Job Object 系统调用）。
		if info.Status == core.StatusRunning {
			if pids := ownedPIDsFromMap(portPIDMap, info.Port, m.cfg.Get().InstallDir, info.Kind, info.Version); len(pids) > 0 {
				adopts = append(adopts, adoptTask{id: id, port: info.Port, pids: pids})
			}
		}

		switch {
		case !changed:
			// 状态未变，不发事件（Adopt 已在上面兜底）
		case info.Status == core.StatusStopped && (old == core.StatusRunning || old == core.StatusError):
			// 进程消失：清掉句柄，允许下次 Start 正常走流程
			s.process = nil
			c := change{id: id, info: info}
			c.events = append(c.events, struct {
				evt   reactive.EventType
				extra map[string]interface{}
			}{reactive.EventServiceStopped, map[string]interface{}{"reason": "process-exited"}})
			changes = append(changes, c)
		case info.Status == core.StatusRunning && (old == core.StatusStopped || old == core.StatusError):
			// 端口上发现本实例进程（外部手动启动、历史孤儿进程等）：认领全部 PID，
			// 只记代表 PID 会让后续判活/停止漏掉同实例的其余进程。
			if s.process == nil || s.process.PID != info.PID {
				pids := ownedPIDsFromMap(portPIDMap, info.Port, m.cfg.Get().InstallDir, info.Kind, info.Version)
				if s.process = processHandleFromPIDs(pids); s.process == nil {
					s.process = &process.ProcessHandle{PID: info.PID, Started: time.Now()}
				}
			}
			c := change{id: id, info: info}
			c.events = append(c.events, struct {
				evt   reactive.EventType
				extra map[string]interface{}
			}{reactive.EventServiceStarted, map[string]interface{}{"reason": "adopted"}})
			changes = append(changes, c)
		}
	}
	m.mu.Unlock()

	// 锁外接管：保证"进程在跑 ⇔ 编排引擎有登记"这条不变式，
	// 让 Stop 永远有能力停掉正在运行的实例，而不是以记账缺失为由拒绝。
	for _, a := range adopts {
		m.adoptOrchestrator(a.id, a.port, a.pids)
	}

	// 锁外发事件，避免 handler 回调死锁
	for _, c := range changes {
		for _, ev := range c.events {
			m.publishServiceEvent(ev.evt, c.id, ev.extra)
		}
	}
}

const maxRestartCount = 3

// stableRunWindow 是"这一轮算正常运行过"的最低在线时长。
const stableRunWindow = 60 * time.Second

// decayRestartCount 决定重启计数是否清零：只有上一轮确实稳定运行过一段时间才清零。
// 原先每次 Start 成功就无条件置 0，于是"起得来但几秒就死"的服务每轮计数都是 1，
// 永远够不到 maxRestartCount 的熔断阈值 —— 表现为无限重启、无退避。
func decayRestartCount(count int, prev *process.ProcessHandle) int {
	if count == 0 {
		return 0
	}
	if prev != nil && !prev.Started.IsZero() && time.Since(prev.Started) >= stableRunWindow {
		return 0
	}
	return count
}

// instanceOnPort 找出同 kind、正占用同一端口、且不是本次要启动的那个实例 ID（用于先停兄弟版本）。
func (m *Manager) instanceOnPort(kind string, port int, exceptID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, s := range m.services {
		if id == exceptID || s.Info.Kind != kind || s.Info.Port != port {
			continue
		}
		if s.Info.Status == core.StatusRunning || s.Info.Status == core.StatusStarting {
			return id
		}
	}
	return ""
}

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

// checkServiceHealth 判断服务是否仍在提供服务。
//
// 不能只认启动时记录的那批 PID：nginx 的监听 socket 由 worker 持有，而
// `nginx -s reload` 会整批更换 worker（master 不变），旧 PID 全部退出。
// 按旧 PID 判活会在每次 reload 后误报故障并触发自动重启——表现为 nginx
// 明明在正常服务，日志却持续出现 service.health_failed。
// 这里以"当前端口上仍在监听、且属于本实例安装目录的进程"为准，
// 再叠加一次 TCP 连接确认端口真的可连。
func (m *Manager) checkServiceHealth(id string, port int) bool {
	if port <= 0 {
		return true
	}

	s := m.GetService(id)
	if s == nil {
		return false
	}
	pids := m.livePIDs(s.Info.Kind, s.Info.Version, port, false)
	if len(pids) == 0 {
		return false
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()

	// 把句柄刷新为当前这一代 PID，避免 UI/停止流程继续使用已退出的 worker。
	m.mu.Lock()
	if cur := m.services[id]; cur != nil {
		cur.Info.PID = pids[0]
		if cur.process != nil {
			cur.process.PID = pids[0]
			cur.process.ExtraPIDs = append(cur.process.ExtraPIDs[:0], pids[1:]...)
		}
	}
	m.mu.Unlock()
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
	// 按端口 + 安装目录归属识别本实例的**全部**进程（不误杀外部程序）：
	// master/worker 结构下漏掉任何一个，目录都会被占用而删不掉。
	if port > 0 {
		for _, occ := range m.livePIDs(kind, version, port, true) {
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

// resolveConfigTarget 把调用方传入的标识解析为服务表里真实存在的完整 ID（kind@version）。
//
// 前端存在只传 kind 的调用（applyServiceConfig('nginx')），多版本共存时不能按 map
// 随机序挑版本，必须确定性选择：运行中 > 注册表默认版本 > 最高版本。
// 解析结果贯穿到后续每一步——此前只在这里选出实例，apply-config 又拿原始入参回查，
// 裸 kind 必然查不到，导致"配置事务失败"。
func (m *Manager) resolveConfigTarget(id string) (string, *ServiceInstance) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if s, ok := m.services[id]; ok {
		return id, s
	}

	kind := kindFromID(id)
	defVer := defaultVersionOf(kind)
	var best *ServiceInstance
	for _, cand := range m.services {
		if cand.Info.Kind != kind {
			continue
		}
		// 运行中的实例优先，且一旦命中即为最终选择。
		if cand.Info.Status == core.StatusRunning {
			return cand.Info.ID, cand
		}
		if best == nil {
			best = cand
			continue
		}
		// 其次注册表默认版本。
		if defVer != "" {
			if cand.Info.Version == defVer {
				best = cand
				continue
			}
			if best.Info.Version == defVer {
				continue
			}
		}
		// 最后取最高版本（按数字段比较，字符串序会把 3.9.13 判得比 3.12.8 高）。
		if versionGreater(cand.Info.Version, best.Info.Version) {
			best = cand
		}
	}
	if best == nil {
		return "", nil
	}
	return best.Info.ID, best
}

// ResolveInstanceID 把可能只含 kind 的标识解析为服务表里的完整 ID，
// 让日志等按 ID 查表的调用方复用同一套版本选择规则。解析不出返回空串。
func (m *Manager) ResolveInstanceID(id string) string {
	target, _ := m.resolveConfigTarget(id)
	return target
}

func (m *Manager) Configure(id string) error {
	targetID, s := m.resolveConfigTarget(id)
	if s == nil {
		return core.ErrServiceNotFound
	}
	handler := m.matchConfigHandler(targetID)
	if handler == nil {
		return nil
	}

	kind := kindFromID(targetID)
	version := s.Info.Version

	compensator := tx.NewCompensator()
	txn := tx.NewTransaction("configure-" + targetID)

	installDir := m.cfg.Get().InstallDir
	configPath := filepath.Join(installDir, "programs", kind, version, "conf")
	backupPath := configPath + ".bak"

	txn.
		Step("backup-config", func(ctx context.Context) error {
			return copyDir(configPath, backupPath)
		}, compensator.RestoreBackup(backupPath, configPath)).
		Step("apply-config", func(ctx context.Context) error {
			m.mu.RLock()
			si := m.services[targetID]
			m.mu.RUnlock()
			if si == nil {
				return core.ErrServiceNotFound
			}
			return handler.Configure(si, m.cfg, m.resourceMgr())
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
		Step("quarantine-dir", func(ctx context.Context) error {
			if !dirExists(binPath) {
				return nil
			}
			// 同父目录内改名是 O(1)，取代"整目录拷贝再删"的双倍磁盘 IO。
			// 先清掉上次失败残留的同名备份，保证改名可重入。
			if dirExists(backupPath) {
				if err := removeAll(backupPath); err != nil {
					return err
				}
			}
			return os.Rename(binPath, backupPath)
		}, func(ctx context.Context) error {
			// 改名后、删除前若失败：把隔离目录改回原位。
			if dirExists(backupPath) && !dirExists(binPath) {
				return os.Rename(backupPath, binPath)
			}
			return nil
		}).
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
			if dirExists(backupPath) {
				return removeAll(backupPath)
			}
			return nil
		}, func(ctx context.Context) error {
			// 删除隔离目录中途失败：把残留改回原位，避免"半删"隐身数据。
			if dirExists(backupPath) && !dirExists(binPath) {
				return os.Rename(backupPath, binPath)
			}
			return nil
		})

	if err := txn.Commit(); err != nil {
		if dirExists(backupPath) && !dirExists(binPath) {
			os.Rename(backupPath, binPath)
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
	core.TypeNginx:     "nginx.exe",
	core.TypeMySQL:     filepath.Join("bin", "mysqld.exe"),
	core.TypeRedis:     "redis-server.exe",
	core.TypeMemcached: "memcached.exe",
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
