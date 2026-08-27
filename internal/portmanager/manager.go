package portmanager

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/resource"
)

// PortStatus 端口状态
type PortStatus struct {
	Port      int
	Owner     string
	InUse     bool
	PID       int
	ExePath   string
	OwnedByUs bool // 进程是否属于我们的安装目录
}

// Manager 统一端口管理器
//
// 整合端口分配（resource.PortPool）、冲突检测（netstat）、归属验证（wmic）。
type Manager struct {
	mu          sync.RWMutex
	pool        *resource.PortPool
	installDir  string // DevStack 安装目录，用于归属验证
	reserveMap  map[int]string // 预留端口（服务名 -> 端口）
}

// NewManager 创建端口管理器
func NewManager(dataDir, installDir string) *Manager {
	return &Manager{
		pool:       resource.NewPortPool(dataDir),
		installDir: installDir,
		reserveMap: make(map[int]string),
	}
}

// Allocate 分配端口
//
// 优先使用 preferred 端口，如果不可用则从池中分配。
// 分配后自动检测冲突并验证归属。
func (m *Manager) Allocate(owner string, preferred int) (int, error) {
	port, err := m.pool.Allocate(owner, preferred)
	if err != nil {
		return 0, err
	}

	// 检测冲突
	if status := m.CheckPort(port); status.InUse && !status.OwnedByUs {
		m.pool.Release(port, owner)
		return 0, fmt.Errorf("端口 %d 被外部进程占用 (PID=%d, %s)", port, status.PID, status.ExePath)
	}

	return port, nil
}

// Release 释放端口
func (m *Manager) Release(port int, owner string) error {
	return m.pool.Release(port, owner)
}

// ReleaseByOwner 按所有者释放所有端口
func (m *Manager) ReleaseByOwner(owner string) {
	m.pool.ReleaseByOwner(owner)
}

// CheckPort 检查单个端口状态
func (m *Manager) CheckPort(port int) *PortStatus {
	status := &PortStatus{
		Port: port,
	}

	// 检查是否在分配表中
	if owner, ok := m.pool.GetOwner(port); ok {
		status.Owner = owner
	}

	// 检查端口是否实际在使用
	pid := process.FindPIDByPort(port)
	if pid > 0 {
		status.InUse = true
		status.PID = pid
		status.ExePath = process.GetExecutablePath(pid)
		status.OwnedByUs = m.belongsToUs(status.ExePath)
	}

	return status
}

// CheckConflict 检查端口冲突
//
// 返回值：
//   - nil: 无冲突
//   - error: 冲突详情
func (m *Manager) CheckConflict(port int, expectedOwner string) error {
	status := m.CheckPort(port)

	if !status.InUse {
		return nil
	}

	// 端口在使用中，检查归属
	if status.OwnedByUs {
		// 属于我们，可能是同一服务重启
		if status.Owner == expectedOwner || status.Owner == "" {
			return nil
		}
		return fmt.Errorf("端口 %d 被 %s 占用 (PID=%d)，期望由 %s 占用",
			port, status.Owner, status.PID, expectedOwner)
	}

	// 被外部进程占用
	return fmt.Errorf("端口 %d 被外部进程占用 (PID=%d, %s)", port, status.PID, status.ExePath)
}

// CheckAllConflicts 批量检查端口冲突
func (m *Manager) CheckAllConflicts(ports []int) []error {
	var errors []error
	for _, port := range ports {
		if err := m.CheckConflict(port, ""); err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}

// FindAvailablePort 在指定范围内查找可用端口
func (m *Manager) FindAvailablePort(owner string, preferred, start, end int) (int, error) {
	candidates := m.candidates(preferred, start, end)
	for _, port := range candidates {
		if m.isAvailable(port) {
			if _, err := m.pool.Allocate(owner, port); err == nil {
				return port, nil
			}
		}
	}
	return 0, fmt.Errorf("端口池耗尽: 无可用端口 [%d-%d]", start, end)
}

// GetOwner 获取端口所有者
func (m *Manager) GetOwner(port int) (string, bool) {
	return m.pool.GetOwner(port)
}

// ListAllocated 列出所有已分配端口
func (m *Manager) ListAllocated() map[int]string {
	return m.pool.ListAllocated()
}

// Reserve 预留端口（不分配，只标记）
func (m *Manager) Reserve(port int, owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reserveMap[port] = owner
}

// Unreserve 取消预留
func (m *Manager) Unreserve(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.reserveMap, port)
}

// IsReserved 检查端口是否被预留
func (m *Manager) IsReserved(port int) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	owner, ok := m.reserveMap[port]
	return owner, ok
}

// Snapshot 返回端口使用快照（用于调试/日志）
func (m *Manager) Snapshot() []PortStatus {
	allocated := m.pool.ListAllocated()
 statuses := make([]PortStatus, 0, len(allocated))

	for port, owner := range allocated {
		status := m.CheckPort(port)
		status.Owner = owner
		statuses = append(statuses, *status)
	}

	return statuses
}

// belongsToUs 检查进程是否属于我们的安装目录
func (m *Manager) belongsToUs(exePath string) bool {
	if m.installDir == "" || exePath == "" {
		return false
	}
	normalizedInstall := strings.ToLower(filepath.Clean(m.installDir))
	normalizedExe := strings.ToLower(filepath.Clean(exePath))
	return strings.HasPrefix(normalizedExe, normalizedInstall)
}

// isAvailable 检查端口是否可用（TCP 探测）
func (m *Manager) isAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// candidates 收集候选端口
func (m *Manager) candidates(preferred, start, end int) []int {
	cands := make([]int, 0, end-start+2)
	if preferred >= start && preferred <= end {
		cands = append(cands, preferred)
	}
	for port := start; port <= end; port++ {
		if port != preferred {
			cands = append(cands, port)
		}
	}
	return cands
}

// WaitForPort 等待端口就绪（用于启动后验证）
func (m *Manager) WaitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.isAvailable(port) {
			// 端口可用意味着没有进程在监听，可能服务还没启动完成
			// 或者服务已停止
			time.Sleep(100 * time.Millisecond)
			continue
		}
		// 端口被占用，说明服务已启动
		return nil
	}
	return fmt.Errorf("等待端口 %d 超时 (%v)", port, timeout)
}

// WaitForPortListening 等待端口开始监听（用于启动后验证）
func (m *Manager) WaitForPortListening(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pid := process.FindPIDByPort(port); pid > 0 {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("端口 %d 在 %v 内未开始监听", port, timeout)
}
