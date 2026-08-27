package process

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcInfo 进程快照条目（来自 CreateToolhelp32Snapshot，单次调用即可获得全量数据）。
type ProcInfo struct {
	PID  int
	PPID int    // 父进程 ID（用于识别 master/worker 等父子关系）
	Name string // 进映像名（如 "nginx.exe"，不含路径）
}

// ListProcesses 返回当前系统中所有存活的进程快照。
//
// 单次内核快照同时给出 PID、父 PID 与映像名，
// 用于"按可执行文件路径归组"或"沿父链找主进程"等场景；
// 避免对每个 PID 单独调用路径查询 API（对受保护进程开销极大）。
func ListProcesses() ([]ProcInfo, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("创建进程快照失败: %w", err)
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var out []ProcInfo
	for err := windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		out = append(out, ProcInfo{
			PID:  int(entry.ProcessID),
			PPID: int(entry.ParentProcessID),
			Name: windows.UTF16ToString(entry.ExeFile[:]),
		})
	}
	return out, nil
}

// ListPIDs 返回当前系统中所有存活的进程 ID。
func ListPIDs() ([]int, error) {
	procs, err := ListProcesses()
	if err != nil {
		return nil, err
	}
	pids := make([]int, len(procs))
	for i, p := range procs {
		pids[i] = p.PID
	}
	return pids, nil
}
