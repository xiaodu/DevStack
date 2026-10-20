package process

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const maxOutputSize = 1024 * 1024 // 1MB per process

type ProcessInfo struct {
	PID     int
	Name    string
	Running bool
	Port    int
	Uptime  time.Duration
	Started time.Time
}

type ProcessTree struct {
	RootPID   int
	ChildPIDs []int
	Started   time.Time
}

// ProcessHandle holds references to one or more related OS processes.
type ProcessHandle struct {
	PID       int   // primary PID
	MasterPID int   // master PID (for services with master/worker model)
	ExtraPIDs []int // additional PIDs (e.g. multiple php-cgi workers)
	Started   time.Time
}

// ProcessOutput stores captured stdout/stderr from a process
type ProcessOutput struct {
	mu     sync.RWMutex
	stdout []byte
	stderr []byte
}

func (po *ProcessOutput) Stdout() []byte {
	po.mu.RLock()
	defer po.mu.RUnlock()
	return po.stdout
}

func (po *ProcessOutput) Stderr() []byte {
	po.mu.RLock()
	defer po.mu.RUnlock()
	return po.stderr
}

func (po *ProcessOutput) appendStdout(data []byte) {
	po.mu.Lock()
	defer po.mu.Unlock()
	po.stdout = appendTrimmed(po.stdout, data, maxOutputSize)
}

func (po *ProcessOutput) appendStderr(data []byte) {
	po.mu.Lock()
	defer po.mu.Unlock()
	po.stderr = appendTrimmed(po.stderr, data, maxOutputSize)
}

// appendTrimmed 追加 data 到 buf，超出 cap 时按完整行裁剪头部，
// 避免滚动缓冲切断中间行导致日志字节丢失。
func appendTrimmed(buf, data []byte, cap int) []byte {
	buf = append(buf, data...)
	if len(buf) <= cap {
		return buf
	}
	// 从尾部向前保留 cap 字节，然后丢弃该窗口开头的半行，保证首行完整
	keepFrom := len(buf) - cap
	if i := bytes.IndexByte(buf[keepFrom:], '\n'); i >= 0 {
		keepFrom += i + 1
	} else {
		// 窗口内无换行（单行超长），只能硬截断
		keepFrom = len(buf) - cap
	}
	if keepFrom <= 0 {
		return buf
	}
	return buf[keepFrom:]
}

type Manager struct {
	mu           sync.Mutex
	processes    map[int]*exec.Cmd
	exitChans    map[int]chan struct{}
	processTrees map[int]*ProcessTree
	outputs      map[int]*ProcessOutput
}

func NewManager() *Manager {
	return &Manager{
		processes:    make(map[int]*exec.Cmd),
		exitChans:    make(map[int]chan struct{}),
		processTrees: make(map[int]*ProcessTree),
		outputs:      make(map[int]*ProcessOutput),
	}
}

// GetOutput returns captured output for a process
func (m *Manager) GetOutput(pid int) *ProcessOutput {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputs[pid]
}

func (m *Manager) Start(name, executable string, args []string, env []string, workdir string) (int, error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir = workdir

	output := &ProcessOutput{}
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Start(); err != nil {
		stdoutWriter.Close()
		stderrWriter.Close()
		return 0, fmt.Errorf("启动 %s 失败: %w", name, err)
	}

	pid := cmd.Process.Pid
	exitCh := make(chan struct{})
	m.mu.Lock()
	m.processes[pid] = cmd
	m.exitChans[pid] = exitCh
	m.outputs[pid] = output
	m.mu.Unlock()

	// 异步捕获 stdout
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdoutReader.Read(buf)
			if n > 0 {
				output.appendStdout(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}()

	// 异步捕获 stderr
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderrReader.Read(buf)
			if n > 0 {
				output.appendStderr(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}()

	go func() {
		cmd.Wait()
		stdoutWriter.Close()
		stderrWriter.Close()
		m.mu.Lock()
		delete(m.processes, pid)
		close(exitCh)
		delete(m.exitChans, pid)
		// 退出即清理输出缓冲，避免高频重启场景（如 php-cgi）下内存无界增长
		delete(m.outputs, pid)
		m.mu.Unlock()
	}()

	return pid, nil
}

func (m *Manager) WaitChan(pid int) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.exitChans[pid]; ok {
		return ch
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}

// Stop 终止指定进程及其子进程（进程树）。
// 统一使用 taskkill /F /T /PID：
//   - /F 强制终止（TerminateProcess 语义）
//   - /T 递归终止子进程，覆盖 nginx master+worker、php-fpm master+children 等多进程模型
//
// 进程已退出视为幂等成功（返回 nil），避免上层处理 spurious error。
// 失败时返回真实原因（含 taskkill stderr），便于上层透传给用户。
// 自管进程（pm.Start 注册的）在 taskkill 触发退出后，cmd.Wait() goroutine
// 仍会正常清理 m.processes / m.exitChans / m.outputs，链路保持完整。
func (m *Manager) Stop(pid int) error {
	if pid <= 0 {
		return nil
	}
	if !IsProcessRunning(pid) {
		return nil
	}
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("终止进程 %d 失败: %w, 输出: %s", pid, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *Manager) IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	m.mu.Lock()
	_, ok := m.processes[pid]
	m.mu.Unlock()
	if ok {
		return true
	}
	return IsProcessRunning(pid)
}

func IsProcessRunning(pid int) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	err = syscall.GetExitCodeProcess(h, &code)
	if err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

// FindPIDsByPort 返回当前监听指定端口的全部进程 PID（可能为空切片）。
//
// 必须精确匹配端口号（复用 FindAllPIDsByPort 的解析），
// 不能用 Contains(":80") 之类的子串匹配——那会把 :8001/:8080 误判为 :80。
//
// 为什么需要集合而不是单值：Windows 上 nginx/mysql/redis 这类服务是
// master/worker 结构，master 与每个 worker 都会出现在 netstat 的 LISTENING
// 行里（worker 继承监听套接字）。任何"一个端口只有一个进程"的假设都会导致
// 只杀掉其中一个 worker，master 立刻重生 worker 抢回端口——停止动作因此
// 从未真正生效。判活与判停必须以"该实例的全部 PID"为单位。
func FindPIDsByPort(port int) []int {
	return append([]int(nil), FindAllPIDsByPort()[port]...)
}

// FindPIDByPort 返回监听指定端口的任意一个进程 PID；无人监听时返回 0。
//
// 只适用于"端口有没有被占"这类布尔判断。凡是涉及停止、复核、归属认定的
// 场景，必须改用 FindPIDsByPort / FindPIDsByPortOwned，否则会漏杀同端口的
// 其余进程。
func FindPIDByPort(port int) int {
	pids := FindAllPIDsByPort()[port]
	if len(pids) == 0 {
		return 0
	}
	return pids[0]
}

// FindPIDsByPortOwned 返回监听在 port 上、且可执行文件路径位于 exeDir 目录内的全部 PID。
//
// 这是"本实例到底有哪些进程在跑"的唯一判据：同端口的 master + 全部 worker 都会
// 被收集，端口被其它程序或其它版本占用时返回空，避免把外部进程误认/误杀为本实例。
func FindPIDsByPortOwned(port int, exeDir string) []int {
	if port <= 0 || exeDir == "" {
		return nil
	}
	dir := strings.ToLower(exeDir)
	var out []int
	for _, pid := range FindPIDsByPort(port) {
		exe := strings.ToLower(GetExecutablePath(pid))
		if exe != "" && strings.Contains(exe, dir) {
			out = append(out, pid)
		}
	}
	return out
}

// executablePathCache 缓存 GetExecutablePath（Win32 API）的查询结果，
// 避免 syncServiceState 为每个服务重复打开进程查路径。
var (
	executablePathCache     map[int]string
	executablePathCacheTime time.Time
	executablePathCacheMu   sync.Mutex
)

// executablePathCacheTTL 是 exe 路径缓存的有效期
const executablePathCacheTTL = 5 * time.Second

// GetExecutablePath 返回指定 PID 进程的可执行文件完整路径。
// 走纯 Win32 API（OpenProcess + QueryFullProcessImageName），不依赖 wmic——
// wmic 在 Windows 11 24H2+ 已被移除，届时此函数会全线失效。
// 查询失败（进程已退出 / 权限不足）时返回空字符串。
func GetExecutablePath(pid int) string {
	if pid <= 0 {
		return ""
	}
	executablePathCacheMu.Lock()
	defer executablePathCacheMu.Unlock()

	if executablePathCache != nil && time.Since(executablePathCacheTime) > executablePathCacheTTL {
		executablePathCache = nil
	}
	if executablePathCache == nil {
		executablePathCache = make(map[int]string)
		executablePathCacheTime = time.Now()
	}
	if p, ok := executablePathCache[pid]; ok {
		return p
	}

	path := queryFullProcessImageName(pid)
	executablePathCache[pid] = path
	return path
}

// queryFullProcessImageName 用 QueryFullProcessImageNameW 查询单个进程的 exe 完整路径。
func queryFullProcessImageName(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

// portPIDCache 缓存 netstat 扫描结果，避免短时间内重复执行 netstat -ano
// 一次 Dashboard.load() 中 GetServices 和 GetEnvironment 都会调用 svcMgr.List()，
// 进而调用 FindAllPIDsByPort()，缓存可以避免重复的 netstat 调用。
//
// 值是切片而不是单值：同端口的 master 与全部 worker 都要保留（见 FindPIDsByPort）。
var (
	portPIDCache     map[int][]int
	portPIDCacheTime time.Time
	portPIDCacheMu   sync.Mutex
)

// portPIDCacheTTL 是端口 PID 缓存的有效期。
// 仅限"展示型"判活使用；停止后的复核必须先 InvalidatePortCache()，
// 否则 200ms 一轮的轮询全程读到停止前的陈旧快照，会把已停成功误报成停止失败。
const portPIDCacheTTL = 2 * time.Second

// InvalidatePortCache 强制下一次端口查询走真实扫描，并作废 exe 路径缓存。
// 强杀/优雅停止之后、复核进程是否退出之前必须调用，否则 2s TTL 内的轮询
// 会全程读到停止前的陈旧快照，把已经停成功的动作误报成停止失败。
//
// 注意只清时间戳不清内容：netstat 偶发失败时仍可降级到上一次成功结果，
// 保持"陈旧好过错误空数据"的策略不变。
func InvalidatePortCache() {
	portPIDCacheMu.Lock()
	portPIDCacheTime = time.Time{}
	portPIDCacheMu.Unlock()

	executablePathCacheMu.Lock()
	executablePathCache = nil
	executablePathCacheTime = time.Time{}
	executablePathCacheMu.Unlock()
}

// stalePortPIDCache netstat 扫描失败时的降级策略：返回上一次成功扫描的缓存（无论多旧）。
// 之前失败直接返回空 map，调用方把"扫描失败"当"没有进程监听"产生假阴性——
// 判活误报已停止、端口冲突误判空闲。陈旧数据远好于错误的空数据；完全没有历史数据时才返回空。
// 必须在持有 portPIDCacheMu 时调用。
func stalePortPIDCache(reason string) map[int][]int {
	if portPIDCache != nil {
		result := make(map[int][]int, len(portPIDCache))
		for k, v := range portPIDCache {
			result[k] = append([]int(nil), v...)
		}
		return result
	}
	_ = reason // 无历史数据可用，调用方将得到空 map（记录 reason 便于调试）
	return nil
}

func FindAllPIDsByPort() map[int][]int {
	portPIDCacheMu.Lock()
	defer portPIDCacheMu.Unlock()

	// 缓存命中：在 TTL 内直接复用
	if portPIDCache != nil && time.Since(portPIDCacheTime) < portPIDCacheTTL {
		result := make(map[int][]int, len(portPIDCache))
		for k, v := range portPIDCache {
			result[k] = append([]int(nil), v...)
		}
		return result
	}

	// 优先读内核 TCP 表（iphlpapi GetExtendedTcpTable），不起外部进程；
	// API 失败才回退 netstat（保底可用）。
	if apiResult, apiErr := listeningPortsViaAPI(); apiErr == nil {
		portPIDCache = apiResult
		portPIDCacheTime = time.Now()
		result := make(map[int][]int, len(apiResult))
		for k, v := range apiResult {
			result[k] = append([]int(nil), v...)
		}
		return result
	}

	// 回退路径：netstat -ano 文本解析（无 GetExtendedTcpTable 可用时）
	cmd := exec.Command("netstat", "-ano")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	done := make(chan struct{}, 1)
	var out []byte
	var err error

	go func() {
		out, err = cmd.Output()
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return stalePortPIDCache("netstat 超时")
	}

	if err != nil {
		return stalePortPIDCache(err.Error())
	}
	result := make(map[int][]int)
	seen := make(map[int]map[int]bool)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "LISTEN") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		addr := fields[1]
		if idx := strings.LastIndex(addr, ":"); idx > 0 {
			portStr := addr[idx+1:]
			if port, err := strconv.Atoi(portStr); err == nil {
				pid, _ := strconv.Atoi(fields[len(fields)-1])
				if pid > 0 {
					// 同端口多 PID（master + 多个 worker 都继承监听套接字）全部保留，
					// IPv4/IPv6 双行会重复报告同一 PID，用 seen 去重。
					if seen[port] == nil {
						seen[port] = make(map[int]bool)
					}
					if !seen[port][pid] {
						seen[port][pid] = true
						result[port] = append(result[port], pid)
					}
				}
			}
		}
	}

	// 更新缓存
	portPIDCache = result
	portPIDCacheTime = time.Now()

	return result
}

func FindExecutable(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	exts := []string{".exe", ".bat", ".cmd", ""}
	paths := strings.Split(os.Getenv("PATH"), ";")
	for _, path := range paths {
		for _, ext := range exts {
			full := filepath.Join(path, name+ext)
			if _, err := os.Stat(full); err == nil {
				return full
			}
		}
	}
	return ""
}

func (m *Manager) RegisterProcessTree(rootPID int) *ProcessTree {
	pt := &ProcessTree{
		RootPID:   rootPID,
		ChildPIDs: findChildProcesses(rootPID),
		Started:   time.Now(),
	}
	m.mu.Lock()
	m.processTrees[rootPID] = pt
	m.mu.Unlock()
	return pt
}

func (m *Manager) GetProcessTree(rootPID int) (*ProcessTree, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pt, ok := m.processTrees[rootPID]
	return pt, ok
}

func (m *Manager) StopProcessTree(rootPID int) error {
	m.mu.Lock()
	pt, ok := m.processTrees[rootPID]
	if ok {
		for _, pid := range pt.ChildPIDs {
			if IsProcessRunning(pid) {
				if proc, err := os.FindProcess(pid); err == nil {
					proc.Kill()
				}
			}
		}
		delete(m.processTrees, rootPID)
	}
	m.mu.Unlock()

	if cmd, ok := m.GetCommand(rootPID); ok {
		return cmd.Process.Kill()
	}

	if IsProcessRunning(rootPID) {
		if proc, err := os.FindProcess(rootPID); err == nil {
			return proc.Kill()
		}
	}
	return nil
}

func (m *Manager) GetCommand(pid int) (*exec.Cmd, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmd, ok := m.processes[pid]
	return cmd, ok
}

// findChildProcesses 返回 parentPID 的全部后代进程 PID（含多级子孙）。
// 使用 Toolhelp32 进程快照按 PPID 树遍历，不再调用 wmic（Win11 24H2+ 已移除）。
func findChildProcesses(parentPID int) []int {
	procs, err := ListProcesses()
	if err != nil {
		return nil
	}
	children := make(map[int][]int, len(procs))
	for _, p := range procs {
		children[p.PPID] = append(children[p.PPID], p.PID)
	}
	var result []int
	var walk func(pid int)
	visited := map[int]struct{}{parentPID: {}}
	walk = func(pid int) {
		for _, child := range children[pid] {
			if _, dup := visited[child]; dup {
				continue // 防御 PPID 环（PID 复用等场景）
			}
			visited[child] = struct{}{}
			result = append(result, child)
			walk(child)
		}
	}
	walk(parentPID)
	return result
}
