package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Job Object 常量定义（对应 Windows SDK winnt.h）
const (
	// JobObjectExtendedLimitInformationClass 用于 SetInformationJobObject
	JobObjectExtendedLimitInformationClass = 9

	// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE 关闭 Job 句柄时自动终止所有进程
	JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x00002000

	// JOB_OBJECT_LIMIT_ACTIVE_PROCESS 限制活跃进程数
	JOB_OBJECT_LIMIT_ACTIVE_PROCESS = 0x00000008

	// JOB_OBJECT_LIMIT_PROCESS_MEMORY 限制单个进程内存
	JOB_OBJECT_LIMIT_PROCESS_MEMORY = 0x00000100

	// JOB_OBJECT_LIMIT_JOB_MEMORY 限制 Job 总内存
	JOB_OBJECT_LIMIT_JOB_MEMORY = 0x00000200

	// JOB_OBJECT_LIMIT_JOB_TIME 限制 Job 总 CPU 时间
	JOB_OBJECT_LIMIT_JOB_TIME = 0x00000004
)

// JOBOBJECT_BASIC_LIMIT_INFORMATION 基本限制信息结构体
type JOBOBJECT_BASIC_LIMIT_INFORMATION struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

// JOBOBJECT_EXTENDED_LIMIT_INFORMATION 扩展限制信息结构体
type JOBOBJECT_EXTENDED_LIMIT_INFORMATION struct {
	BasicLimitInformation JOBOBJECT_BASIC_LIMIT_INFORMATION
	IoInfo                windows.IO_COUNTERS
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// Group 进程组管理（使用 Windows Job Object）
//
// 每个 Group 对应一个 Windows Job Object，管理一组相关进程。
// 当 Group 关闭时，所有进程（包括子进程的子进程）自动终止。
//
// 设计目标：
// 1. 内核级进程树管理，解决 taskkill /F /T 误杀问题
// 2. 父进程崩溃时，子进程自动清理
// 3. 支持优雅关闭（先发信号，超时再强制终止）
type Group struct {
	mu     sync.Mutex
	handle windows.Handle     // Windows Job Object 句柄
	name   string             // Job Object 名称（用于调试）
	procs  map[int]*GroupProc // PID -> 进程信息
	closed bool               // 是否已关闭
}

// GroupProc 组内进程信息
type GroupProc struct {
	PID       int
	Cmd       *exec.Cmd
	Name      string
	Started   time.Time
	GraceSecs int // 优雅关闭等待秒数，默认 5
}

// NewGroup 创建一个新的进程组（Job Object）
//
// 返回的 Group 使用 JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE 标志，
// 确保 Group.Close() 被调用时自动终止所有进程。
// 即使调用方忘记关闭，进程退出时 Job Object 句柄也会被释放，
// 子进程也会被终止。
func NewGroup(name string) (*Group, error) {
	// 创建 Job Object，名称为空表示匿名
	hJob, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 Job Object 失败: %w", err)
	}

	// 配置扩展限制信息
	var limitInfo JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limitInfo.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	// 将结构体转换为指针传递给 SetInformationJobObject
	limitInfoPtr := unsafe.Pointer(&limitInfo)
	_, err = windows.SetInformationJobObject(
		hJob,
		JobObjectExtendedLimitInformationClass,
		uintptr(limitInfoPtr),
		uint32(unsafe.Sizeof(limitInfo)),
	)
	if err != nil {
		windows.CloseHandle(windows.Handle(hJob))
		return nil, fmt.Errorf("配置 Job Object 失败: %w", err)
	}

	return &Group{
		handle: hJob,
		name:   name,
		procs:  make(map[int]*GroupProc),
	}, nil
}

// Start 启动进程并加入进程组
//
// 参数：
//   - name: 进程名称（用于日志和调试）
//   - executable: 可执行文件路径
//   - args: 命令行参数
//   - env: 环境变量（nil 表示继承父进程环境）
//   - workdir: 工作目录
//   - graceSecs: 优雅关闭等待秒数（默认 5）
//
// 返回：
//   - *GroupProc: 进程信息
//   - error: 启动失败时返回错误
//
// 流程：
//  1. 创建 exec.Cmd 对象
//  2. 调用 cmd.Start() 启动进程（此时进程已创建）
//  3. 将进程加入 Job Object（AssignProcessToJobObject）
//  4. 注册到 Group 的进程表中
//
// 注意：如果进程已在另一个 Job 中，AssignProcessToJobObject 会失败。
// 此时返回错误，由调用方决定是否回退到 taskkill 模式。
func (g *Group) Start(name, executable string, args []string, env []string, workdir string, graceSecs int) (*GroupProc, error) {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil, fmt.Errorf("进程组 %s 已关闭", g.name)
	}
	g.mu.Unlock()

	if graceSecs <= 0 {
		graceSecs = 5
	}

	cmd := exec.Command(executable, args...)
	cmd.Dir = workdir

	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	// HideWindow 防止弹出控制台窗口
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	// 启动进程
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动进程 %s 失败: %w", name, err)
	}

	pid := cmd.Process.Pid

	// 将进程加入 Job Object
	if err := g.assignProcess(pid); err != nil {
		// 加入 Job 失败，先杀掉刚启动的进程
		killProcess(pid)
		return nil, fmt.Errorf("将进程 %s (PID=%d) 加入进程组失败: %w", name, pid, err)
	}

	proc := &GroupProc{
		PID:       pid,
		Cmd:       cmd,
		Name:      name,
		Started:   time.Now(),
		GraceSecs: graceSecs,
	}

	g.mu.Lock()
	g.procs[pid] = proc
	g.mu.Unlock()

	// 后台等待进程退出，从进程表中移除
	go func() {
		cmd.Wait()
		g.mu.Lock()
		delete(g.procs, pid)
		g.mu.Unlock()
	}()

	return proc, nil
}

// AssignPID 将已运行的进程加入 Job Object
//
// 用于 provider 已启动进程后，将其纳入 Job Object 管理。
// 进程退出后自动从进程表中移除。
func (g *Group) AssignPID(pid int, name string, graceSecs int) error {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return fmt.Errorf("进程组 %s 已关闭", g.name)
	}
	g.mu.Unlock()

	if graceSecs <= 0 {
		graceSecs = 5
	}

	if !isProcessRunning(pid) {
		return fmt.Errorf("进程 %d 不存在或已退出", pid)
	}

	// 将进程加入 Job Object
	if err := g.assignProcess(pid); err != nil {
		return fmt.Errorf("将进程 %s (PID=%d) 加入进程组失败: %w", name, pid, err)
	}

	proc := &GroupProc{
		PID:       pid,
		Name:      name,
		Started:   time.Now(),
		GraceSecs: graceSecs,
	}

	g.mu.Lock()
	g.procs[pid] = proc
	g.mu.Unlock()

	// 后台等待进程退出，从进程表中移除。
	// 用 WaitSingleObject 阻塞在进程句柄上（进程退出时内核置 signaled），
	// 替代此前每 500ms 轮询 isProcessRunning 的常驻 goroutine：消除稳态轮询开销，
	// 且句柄绑定的是这个进程对象本身，天然免疫 PID 复用误判。
	go g.awaitExit(pid)

	return nil
}

// awaitExit 阻塞等待进程退出后将其移出进程表。
// 打不开句柄（进程已退出或权限不足）时直接清理，语义等价于轮询发现进程已不在。
func (g *Group) awaitExit(pid int) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		g.removeProc(pid)
		return
	}
	defer windows.CloseHandle(h)
	// INFINITE：进程句柄仅在进程终止时 signaled，等待期间进程若存活就该一直阻塞，
	// 不存在“永不返回却仍在表内”的窗口；进程退出后立即清理。
	_, _ = windows.WaitForSingleObject(h, windows.INFINITE)
	g.removeProc(pid)
}

// removeProc 从进程表移除指定 PID（幂等，缺失即为 no-op）。
func (g *Group) removeProc(pid int) {
	g.mu.Lock()
	delete(g.procs, pid)
	g.mu.Unlock()
}

// ReleasePID 从 Job Object 中移除进程（不终止进程）
func (g *Group) ReleasePID(pid int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.procs, pid)
}

// assignProcess 将进程加入 Job Object
//
// 原理：调用 Windows API AssignProcessToJobObject，
// 将指定 PID 的进程加入当前 Job Object。
//
// 注意：一个进程只能属于一个 Job Object。
// 如果进程已经在另一个 Job 中，调用会失败。
func (g *Group) assignProcess(pid int) error {
	// 打开进程，需要 PROCESS_SET_QUOTA 和 PROCESS_TERMINATE 权限
	hProcess, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		return fmt.Errorf("打开进程 %d 失败: %w", pid, err)
	}
	defer windows.CloseHandle(hProcess)

	// 将进程加入 Job Object
	if err := windows.AssignProcessToJobObject(g.handle, hProcess); err != nil {
		return fmt.Errorf("AssignProcessToJobObject 失败: %w", err)
	}

	return nil
}

// Stop 优雅停止指定进程
//
// 流程：
//  1. 尝试关闭窗口式优雅退出（taskkill 不带 /F，对有窗口的进程等价 WM_CLOSE）
//  2. 等待 graceSecs 秒
//  3. 如果进程仍在运行，强制终止
//
// 历史实现用 GenerateConsoleCtrlEvent 发 Ctrl+C，但服务进程由
// DETACHED/CREATE_NEW_PROCESS_GROUP 方式启动、与本进程无共享控制台，
// 信号永远送不到目标（或发给自己），是“假优雅关闭”的源头，已删除。
func (g *Group) Stop(pid int) error {
	g.mu.Lock()
	proc, ok := g.procs[pid]
	g.mu.Unlock()

	if !ok {
		return fmt.Errorf("进程 %d 不在进程组 %s 中", pid, g.name)
	}

	if !isProcessRunning(pid) {
		return nil
	}

	// 1. 尝试优雅关闭：无窗口/无响应时直接失败，下一步强制终止
	if err := fallbackSendClose(pid); err != nil {
		// 优雅关闭请求发不出去，直接强制终止
		return g.ForceStop(pid)
	}

	// 2. 等待优雅关闭
	deadline := time.After(time.Duration(proc.GraceSecs) * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			// 超时，强制终止
			return g.ForceStop(pid)
		case <-ticker.C:
			if !isProcessRunning(pid) {
				return nil
			}
		}
	}
}

// ForceStop 强制终止指定进程
//
// 使用 TerminateProcess API 直接终止进程。
// 这是最后的手段，不发送任何信号。
func (g *Group) ForceStop(pid int) error {
	hProcess, err := windows.OpenProcess(
		windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		return fmt.Errorf("打开进程 %d 失败: %w", pid, err)
	}
	defer windows.CloseHandle(hProcess)

	if err := windows.TerminateProcess(hProcess, 1); err != nil {
		return fmt.Errorf("终止进程 %d 失败: %w", pid, err)
	}

	return nil
}

// StopAll 优雅停止所有进程
//
// 按启动时间倒序停止（后启动的先停止）。
// 每个进程独立处理，某个进程停止失败不影响其他进程。
func (g *Group) StopAll() []error {
	g.mu.Lock()
	procs := make([]*GroupProc, 0, len(g.procs))
	for _, p := range g.procs {
		procs = append(procs, p)
	}
	g.mu.Unlock()

	// 按启动时间倒序
	for i := len(procs) - 1; i >= 0; i-- {
		for j := 0; j < i; j++ {
			if procs[j].Started.After(procs[j+1].Started) {
				procs[j], procs[j+1] = procs[j+1], procs[j]
			}
		}
	}

	var errs []error
	for _, p := range procs {
		if err := g.Stop(p.PID); err != nil {
			errs = append(errs, fmt.Errorf("停止 %s (PID=%d) 失败: %w", p.Name, p.PID, err))
		}
	}

	return errs
}

// ForceStopAll 强制停止所有进程
func (g *Group) ForceStopAll() []error {
	g.mu.Lock()
	procs := make([]*GroupProc, 0, len(g.procs))
	for _, p := range g.procs {
		procs = append(procs, p)
	}
	g.mu.Unlock()

	var errs []error
	for _, p := range procs {
		if err := g.ForceStop(p.PID); err != nil {
			errs = append(errs, fmt.Errorf("强制停止 %s (PID=%d) 失败: %w", p.Name, p.PID, err))
		}
	}

	return errs
}

// Close 关闭进程组
//
// 关闭 Job Object 句柄，触发 JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE，
// 自动终止所有进程（包括子进程的子进程）。
//
// 这是最终的清理手段，确保不会留下孤儿进程。
func (g *Group) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return nil
	}
	g.closed = true

	// 关闭 Job Object 句柄
	// 此时所有进程会被自动终止
	if g.handle != 0 {
		windows.CloseHandle(windows.Handle(g.handle))
		g.handle = 0
	}

	return nil
}

// List 返回当前组内所有进程
func (g *Group) List() []*GroupProc {
	g.mu.Lock()
	defer g.mu.Unlock()

	result := make([]*GroupProc, 0, len(g.procs))
	for _, p := range g.procs {
		result = append(result, p)
	}
	return result
}

// Count 返回当前组内进程数
func (g *Group) Count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.procs)
}

// IsEmpty 检查进程组是否为空
func (g *Group) IsEmpty() bool {
	return g.Count() == 0
}

// Name 返回进程组名称
func (g *Group) Name() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.name
}

// HasPID 检查进程是否在组内
func (g *Group) HasPID(pid int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.procs[pid]
	return ok
}

// ---------- Windows API 辅助函数 ----------

// fallbackSendClose 使用 taskkill /PID 发送关闭请求（不带 /F，允许优雅关闭）。
// 对有窗口的进程等价于 WM_CLOSE；控制台进程无窗口时 taskkill 会报错，
// 调用方据此立即转 ForceStop——不再假装“发送了 Ctrl+C”。
func fallbackSendClose(pid int) error {
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill /PID %d 失败: %w, 输出: %s", pid, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RequestClose 向进程发送「优雅关闭」请求（taskkill 不带 /F），供 provider 的
// 分级停止阶梯中「进程级关闭」一级使用。
//
// 语义与限制（与内部 fallbackSendClose 一致，只是导出给上层）：
//   - 有窗口、可接收 WM_CLOSE 的进程：进入正常关闭流程；
//   - 控制台/服务进程（mysqld、nginx、redis-server 都属此类，无窗口）：
//     taskkill 会直接报错，调用方据此**立即降级到强杀**，不能把它当成
//     “信号已发出”而白等；
//   - 不再尝试 GenerateConsoleCtrlEvent/Ctrl+C：这些进程与 DevStack 不共享
//     控制台，信号永远送不到（已删除的 sendCtrlC 就是在假装送得到）。
//
// 自带 5s 超时，避免 taskkill 自身挂起把整条停止阶梯拖死。
func RequestClose(pid int) error {
	if pid <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill /PID %d 失败: %w, 输出: %s", pid, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// killProcess 强制终止进程
func killProcess(pid int) {
	hProcess, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(hProcess)
	windows.TerminateProcess(hProcess, 1)
}

// isProcessRunning 检查进程是否仍在运行
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	err = windows.GetExitCodeProcess(h, &exitCode)
	if err != nil {
		return false
	}

	// STILL_ACTIVE = 259
	return exitCode == 259
}

// windowsOpenProcess 打开进程并返回句柄
func windowsOpenProcess(pid int) (uintptr, error) {
	h, err := windows.OpenProcess(
		windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		return 0, err
	}
	return uintptr(h), nil
}

// windowsCloseHandle 关闭句柄
func windowsCloseHandle(h uintptr) {
	windows.CloseHandle(windows.Handle(h))
}

// windowsGetExitCodeProcess 获取进程退出码
func windowsGetExitCodeProcess(h uintptr) (uint32, error) {
	var exitCode uint32
	err := windows.GetExitCodeProcess(windows.Handle(h), &exitCode)
	return exitCode, err
}
