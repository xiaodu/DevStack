package runtime

import (
	"fmt"
	"os"
	"path/filepath"
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
}

func NewManager(cfg *config.Config, procMgr *process.Manager, resMgr *resource.Manager) *Manager {
	m := &Manager{
		cfg:       cfg,
		procMgr:   procMgr,
		resMgr:    resMgr,
		instances: make(map[string]*Instance),
		idByType:  make(map[core.RuntimeType][]string),
	}
	m.scan()
	return m
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

func (m *Manager) scan() {
	installDir := m.cfg.Get().InstallDir
	if installDir == "" {
		return
	}

	AutoAssignPorts(m.cfg, installDir, m.resMgr.Ports())

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

			id := string(rt) + "-" + version

			displayName := typeNames[rt]
			if displayName == "" {
				displayName = string(rt)
			}

			info := core.RuntimeInfo{
				ID:        id,
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

			inst := &Instance{Info: info}
			m.instances[id] = inst
			m.idByType[rt] = append(m.idByType[rt], id)
		}
	}
}

func (m *Manager) Start(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inst, ok := m.instances[id]
	if !ok {
		return core.ErrRuntimeNotFound
	}
	if inst.Info.Status == core.StatusRunning {
		return nil
	}

	switch inst.Info.Type {
	case core.RuntimePHP:
		return m.startPHP(inst)
	default:
		return fmt.Errorf("不支持的运行时类型: %s", inst.Info.Type)
	}
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inst, ok := m.instances[id]
	if !ok {
		return core.ErrRuntimeNotFound
	}
	if inst.Info.Status == core.StatusStopped {
		return nil
	}

	switch inst.Info.Type {
	case core.RuntimePHP:
		return m.stopPHP(inst)
	default:
		return fmt.Errorf("不支持的运行时类型: %s", inst.Info.Type)
	}
}

func (m *Manager) startPHP(inst *Instance) error {
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

	inst.Info.Status = core.StatusStarting

	AutoAssignPorts(m.cfg, installDir, m.resMgr.Ports())
	port := GetPHPVersionPort(m.cfg, version)
	inst.Info.Port = port

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
