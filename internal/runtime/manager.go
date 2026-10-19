package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/resource"
)

type Manager struct {
	mu        sync.RWMutex
	cfg       *config.Config
	procMgr   *process.Manager
	resMgr    *resource.Manager
	instances map[string]*Instance
	idByType  map[core.RuntimeType][]string
	// job 将运行时进程纳入 Job Object：应用崩溃（非正常退出）时由 OS 兜底回收，
	// 不再产生孤儿 php-cgi/node 进程。正常退出走 Shutdown 的优雅停止。
	job *process.Group
}

func NewManager(cfg *config.Config, procMgr *process.Manager, resMgr *resource.Manager) *Manager {
	m := &Manager{
		cfg:       cfg,
		procMgr:   procMgr,
		resMgr:    resMgr,
		instances: make(map[string]*Instance),
		idByType:  make(map[core.RuntimeType][]string),
	}
	// Job Object 创建失败不阻断初始化，只是少了崩溃兜底
	if group, err := process.NewGroup("devstack-runtime"); err == nil {
		m.job = group
	}
	m.scan()
	return m
}

// assignJob 将刚拉起的运行时进程纳入 Job Object（应用崩溃时 OS 兜底回收）。
func (m *Manager) assignJob(pid int, name string) {
	if m.job == nil || pid <= 0 {
		return
	}
	if err := m.job.AssignPID(pid, name, 5); err != nil {
		// 纳管失败不影响启动，仅失去崩溃兜底
		_ = err
	}
}

// Shutdown 停止全部运行中的运行时实例并关闭 Job Object。
func (m *Manager) Shutdown() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.instances))
	for id, inst := range m.instances {
		if inst.Info.Status == core.StatusRunning || inst.Info.Status == core.StatusStarting {
			ids = append(ids, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range ids {
		if err := m.Stop(id); err != nil {
			// 尽力而为：单个失败不阻断其余清理
			continue
		}
	}
	if m.job != nil {
		_ = m.job.Close()
	}
}

func (m *Manager) List() []core.RuntimeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]core.RuntimeInfo, 0, len(m.instances))
	typeOrder := []core.RuntimeType{core.RuntimePHP, core.RuntimeNode, core.RuntimePython}

	for _, rt := range typeOrder {
		for _, id := range m.idByType[rt] {
			inst := m.instances[id]
			result = append(result, inst.Info)
		}
	}
	return result
}

func (m *Manager) Get(id string) (core.RuntimeInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	inst, ok := m.instances[id]
	if !ok {
		return core.RuntimeInfo{}, core.ErrRuntimeNotFound
	}

	return inst.Info, nil
}

func (m *Manager) GetPHPVersionPort(version string) int {
	return GetPHPVersionPort(m.cfg, version)
}

// SetResourceManager 注入资源管理器：首跑向导完成前安装目录为空、资源管理器尚未创建，
// 向导结束后必须补注入（见 app.reinitManagers），否则启动 php/node 取端口会空指针。
func (m *Manager) SetResourceManager(rm *resource.Manager) {
	m.mu.Lock()
	m.resMgr = rm
	m.mu.Unlock()
	// 注入后立即补一次此前跳过的端口预分配
	m.scan()
}

// resourceMgr 读取当前资源管理器（受同一把锁保护）。
func (m *Manager) resourceMgr() *resource.Manager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.resMgr
}

// nodePort 返回 Node 版本对应的持久化端口；首次调用时从端口池分配。
func (m *Manager) nodePort(version string) int {
	rm := m.resourceMgr()
	if rm == nil {
		return 0
	}
	allocated := rm.Ports().ListAllocated()
	for port, owner := range allocated {
		if owner == "node:"+version {
			return port
		}
	}
	port, err := rm.Ports().Allocate("node:"+version, 0)
	if err != nil {
		return 0
	}
	return port
}

func (m *Manager) scan() {
	installDir := m.cfg.Get().InstallDir
	if installDir == "" {
		return
	}
	rm := m.resourceMgr()
	if rm == nil {
		// 首跑向导之前资源管理器尚未创建（安装目录为空），跳过端口预分配
		return
	}

	AutoAssignPorts(m.cfg, installDir, rm.Ports())

	typeDirs := map[core.RuntimeType]string{
		core.RuntimePHP:    filepath.Join(installDir, "programs", "php"),
		core.RuntimeNode:   filepath.Join(installDir, "programs", "node"),
		core.RuntimePython: filepath.Join(installDir, "programs", "python"),
	}

	phpPorts := make(map[string]int)
	for _, v := range LoadPHPRegistry(m.cfg).Versions {
		phpPorts[v.Version] = v.Port
	}

	typeNames := map[core.RuntimeType]string{
		core.RuntimePHP:    "PHP",
		core.RuntimeNode:   "Node.js",
		core.RuntimePython: "Python",
	}

	for rt, dir := range typeDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			version := entry.Name()
			rPath := filepath.Join(dir, version)

			id := string(rt) + "@" + version

			displayName := typeNames[rt]
			if displayName == "" {
				displayName = string(rt)
			}

			info := core.RuntimeInfo{
				ID:        id,
				Kind:      string(rt),
				Type:      rt,
				Version:   version,
				Path:      rPath,
				Active:    false,
				Running:   false,
				Installed: true,
				Status:    core.StatusStopped,
				Name:      displayName + " " + version,
			}

			if rt == core.RuntimePHP {
				if port, ok := phpPorts[version]; ok {
					info.Port = port
				}
			}

			if rt == core.RuntimeNode {
				info.Port = m.nodePort(version)
			}

			inst := &Instance{Info: info}
			m.instances[id] = inst
			m.idByType[rt] = append(m.idByType[rt], id)
		}
	}
}

func (m *Manager) Start(id string) error {
	m.mu.Lock()
	inst, ok := m.instances[id]
	if !ok {
		m.mu.Unlock()
		return core.ErrRuntimeNotFound
	}
	if inst.Info.Status == core.StatusRunning {
		m.mu.Unlock()
		return nil
	}
	inst.Info.Status = core.StatusStarting
	m.mu.Unlock()

	var err error
	switch inst.Info.Type {
	case core.RuntimePHP:
		err = m.startPHP(inst)
	case core.RuntimeNode:
		err = m.startNode(inst)
	default:
		err = fmt.Errorf("不支持的运行时类型: %s", inst.Info.Type)
	}

	if err != nil {
		m.mu.Lock()
		inst.Info.Status = core.StatusError
		m.mu.Unlock()
	}
	return err
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	inst, ok := m.instances[id]
	if !ok {
		m.mu.Unlock()
		return core.ErrRuntimeNotFound
	}
	if inst.Info.Status == core.StatusStopped {
		m.mu.Unlock()
		return nil
	}
	inst.Info.Status = core.StatusStopping
	m.mu.Unlock()

	var err error
	switch inst.Info.Type {
	case core.RuntimePHP:
		err = m.stopPHP(inst)
	case core.RuntimeNode:
		err = m.stopNode(inst)
	default:
		err = fmt.Errorf("不支持的运行时类型: %s", inst.Info.Type)
	}

	if err != nil {
		m.mu.Lock()
		inst.Info.Status = core.StatusError
		m.mu.Unlock()
	}
	return err
}

func (m *Manager) Scan() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.instances = make(map[string]*Instance)
	m.idByType = make(map[core.RuntimeType][]string)
	m.scan()
}

func (m *Manager) startPHP(inst *Instance) error {
	rm := m.resourceMgr()
	if rm == nil {
		return fmt.Errorf("资源管理器未初始化，无法为 PHP 分配端口")
	}
	installDir := m.cfg.Get().InstallDir
	version := inst.Info.Version
	phpDir := filepath.Join(installDir, "programs", "php", version)
	phpExe := filepath.Join(phpDir, "php-cgi.exe")
	if !fileExists(phpExe) {
		return fmt.Errorf("php-cgi.exe 未找到: %s", phpExe)
	}

	phpIni := filepath.Join(phpDir, "php.ini")
	if !fileExists(phpIni) {
		ini := GeneratePHPIniContent(m.cfg, phpDir, version)
		os.WriteFile(phpIni, []byte(ini), 0644)
	}

	AutoAssignPorts(m.cfg, installDir, rm.Ports())
	port := GetPHPVersionPort(m.cfg, version)
	inst.Info.Port = port

	// 幂等：基端口已被同版本 php-cgi 占用时（上次启动残留）直接接管，避免重复拉起
	basePID := process.FindPIDByPort(port)
	if basePID > 0 {
		exe := strings.ToLower(process.GetExecutablePath(basePID))
		if strings.Contains(exe, "php-cgi.exe") && strings.Contains(exe, strings.ToLower(version)) {
			var adopt []int
			for i := 0; i < phpWorkerCount; i++ {
				if p := process.FindPIDByPort(port + i); p > 0 {
					adopt = append(adopt, p)
				}
			}
			inst.process = &process.ProcessHandle{
				PID:       basePID,
				ExtraPIDs: adopt,
				Started:   time.Now(),
			}
			inst.Info.Status = core.StatusRunning
			inst.Info.Running = true
			inst.Info.PID = basePID
			return nil
		}
	}

	inst.Info.Status = core.StatusStarting

	phpEnv := []string{
		"PHP_FCGI_MAX_REQUESTS=300",
		"PHP_FCGI_CHILDREN=1",
	}

	var wg sync.WaitGroup
	errCh := make(chan error, phpWorkerCount)
	pidCh := make(chan int, phpWorkerCount)

	for i := 0; i < phpWorkerCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			p := port + index
			args := []string{
				"-b", fmt.Sprintf("127.0.0.1:%d", p),
				"-c", phpIni,
			}
			pid, err := m.procMgr.Start("php", phpExe, args, phpEnv, phpDir)
			if err != nil {
				errCh <- err
				return
			}
			pidCh <- pid
		}(i)
	}

	wg.Wait()
	close(errCh)
	close(pidCh)

	var pids []int
	for pid := range pidCh {
		pids = append(pids, pid)
	}

	for err := range errCh {
		for _, pid := range pids {
			m.procMgr.Stop(pid)
		}
		inst.Info.Status = core.StatusError
		return err
	}

	for i := 0; i < 12; i++ {
		allRunning := true
		for _, pid := range pids {
			if !m.procMgr.IsRunning(pid) {
				allRunning = false
				break
			}
		}
		if allRunning {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	for _, pid := range pids {
		if !m.procMgr.IsRunning(pid) {
			for _, p := range pids {
				m.procMgr.Stop(p)
			}
			inst.Info.Status = core.StatusError
			return fmt.Errorf("PHP 进程启动后立即退出")
		}
	}

	inst.process = &process.ProcessHandle{
		PID:       pids[0],
		ExtraPIDs: pids[1:],
		Started:   time.Now(),
	}
	inst.Info.Status = core.StatusRunning
	inst.Info.Running = true
	inst.Info.PID = pids[0]

	// 全部 worker 纳入 Job Object（崩溃兜底）
	for _, pid := range pids {
		m.assignJob(pid, "php")
	}

	return nil
}

func (m *Manager) stopPHP(inst *Instance) error {
	if inst.process == nil || inst.process.PID <= 0 {
		return nil
	}
	inst.Info.Status = core.StatusStopping

	pids := append([]int{inst.process.PID}, inst.process.ExtraPIDs...)
	for _, pid := range pids {
		m.procMgr.Stop(pid)
	}

	for i := 0; i < 6; i++ {
		allDead := true
		for _, pid := range pids {
			if m.procMgr.IsRunning(pid) {
				allDead = false
				break
			}
		}
		if allDead {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	inst.process = nil
	inst.Info.Status = core.StatusStopped
	inst.Info.Running = false
	inst.Info.PID = 0
	return nil
}
