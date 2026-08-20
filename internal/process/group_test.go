package process

import (
	"os"
	"testing"
	"time"
)

func TestNewGroup(t *testing.T) {
	group, err := NewGroup("test-group")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}
	defer group.Close()

	if group.Name() != "test-group" {
		t.Errorf("expected name 'test-group', got '%s'", group.Name())
	}

	if !group.IsEmpty() {
		t.Error("expected empty group")
	}
}

func TestGroupStart(t *testing.T) {
	group, err := NewGroup("test-start")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}
	defer group.Close()

	// 启动一个简单的进程（notepad 会在后台运行）
	proc, err := group.Start("notepad", "notepad.exe", nil, nil, "", 3)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if proc.PID <= 0 {
		t.Errorf("expected valid PID, got %d", proc.PID)
	}

	if proc.Name != "notepad" {
		t.Errorf("expected name 'notepad', got '%s'", proc.Name)
	}

	if group.Count() != 1 {
		t.Errorf("expected 1 process, got %d", group.Count())
	}

	// 清理
	group.ForceStop(proc.PID)
	time.Sleep(100 * time.Millisecond)
}

func TestGroupStop(t *testing.T) {
	group, err := NewGroup("test-stop")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}
	defer group.Close()

	// 启动进程
	proc, err := group.Start("notepad", "notepad.exe", nil, nil, "", 3)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 停止进程
	if err := group.Stop(proc.PID); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if IsProcessRunning(proc.PID) {
		t.Error("process should have been stopped")
	}
}

func TestGroupForceStop(t *testing.T) {
	group, err := NewGroup("test-force-stop")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}
	defer group.Close()

	// 启动进程
	proc, err := group.Start("notepad", "notepad.exe", nil, nil, "", 3)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 强制停止
	if err := group.ForceStop(proc.PID); err != nil {
		t.Fatalf("ForceStop failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if IsProcessRunning(proc.PID) {
		t.Error("process should have been force stopped")
	}
}

func TestGroupClose(t *testing.T) {
	group, err := NewGroup("test-close")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}

	// 启动多个进程
	proc1, err := group.Start("notepad1", "notepad.exe", nil, nil, "", 3)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	proc2, err := group.Start("notepad2", "notepad.exe", nil, nil, "", 3)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 关闭进程组
	if err := group.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// 验证进程被终止
	if IsProcessRunning(proc1.PID) {
		t.Error("process1 should have been terminated")
	}

	if IsProcessRunning(proc2.PID) {
		t.Error("process2 should have been terminated")
	}
}

func TestGroupList(t *testing.T) {
	group, err := NewGroup("test-list")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}
	defer group.Close()

	// 启动进程
	proc1, err := group.Start("notepad1", "notepad.exe", nil, nil, "", 3)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	proc2, err := group.Start("notepad2", "notepad.exe", nil, nil, "", 3)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 获取列表
	procs := group.List()
	if len(procs) != 2 {
		t.Errorf("expected 2 processes, got %d", len(procs))
	}

	// 验证 PID 存在
	pids := map[int]bool{proc1.PID: true, proc2.PID: true}
	for _, p := range procs {
		if !pids[p.PID] {
			t.Errorf("unexpected PID %d", p.PID)
		}
	}

	// 清理
	group.ForceStop(proc1.PID)
	group.ForceStop(proc2.PID)
	time.Sleep(100 * time.Millisecond)
}

func TestGroupHasPID(t *testing.T) {
	group, err := NewGroup("test-has-pid")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}
	defer group.Close()

	// 启动进程
	proc, err := group.Start("notepad", "notepad.exe", nil, nil, "", 3)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 检查 PID
	if !group.HasPID(proc.PID) {
		t.Error("expected HasPID to return true")
	}

	if group.HasPID(99999) {
		t.Error("expected HasPID to return false")
	}

	// 清理
	group.ForceStop(proc.PID)
	time.Sleep(100 * time.Millisecond)
}

func TestGroupIsEmpty(t *testing.T) {
	group, err := NewGroup("test-is-empty")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}
	defer group.Close()

	if !group.IsEmpty() {
		t.Error("expected empty group")
	}

	// 启动进程
	proc, err := group.Start("notepad", "notepad.exe", nil, nil, "", 3)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if group.IsEmpty() {
		t.Error("expected non-empty group")
	}

	// 清理
	group.ForceStop(proc.PID)
	time.Sleep(100 * time.Millisecond)
}

func TestProcessRunning(t *testing.T) {
	// 测试当前进程（应该在运行）
	if !IsProcessRunning(os.Getpid()) {
		t.Error("current process should be running")
	}

	// 测试不存在的进程
	if IsProcessRunning(99999) {
		t.Error("PID 99999 should not be running")
	}
}

func TestGroupAssignPID(t *testing.T) {
	group, err := NewGroup("test-assign")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}
	defer group.Close()

	// 启动一个长时间运行的进程
	proc, err := group.Start("ping", "ping.exe", []string{"-n", "30", "127.0.0.1"}, nil, ".", 5)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	pid := proc.PID

	// 关闭 group（终止进程）
	if err := group.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 等待进程退出
	time.Sleep(1 * time.Second)
	if IsProcessRunning(pid) {
		t.Fatalf("ping PID=%d should have been killed", pid)
	}

	// 创建新的 group2，启动一个不属于任何 job 的进程（模拟 provider 启动的进程）
	group2, err := NewGroup("test-assign-2")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}
	defer group2.Close()

	proc2, err := group2.Start("ping2", "ping.exe", []string{"-n", "30", "127.0.0.1"}, nil, ".", 5)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	pid2 := proc2.PID

	// 用另一个 group 将该进程纳入管理
	group3, err := NewGroup("test-assign-3")
	if err != nil {
		t.Fatalf("NewGroup failed: %v", err)
	}

	if err := group3.AssignPID(pid2, "ping2-assigned", 5); err != nil {
		t.Fatalf("AssignPID failed: %v", err)
	}

	// 验证进程已加入 group3
	if !group3.HasPID(pid2) {
		t.Errorf("AssignPID: PID %d should be in group", pid2)
	}

	// 通过 group2 关闭（group2 是原始 group，仍持有进程句柄）
	if err := group2.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	time.Sleep(1 * time.Second)
	if IsProcessRunning(pid2) {
		t.Errorf("ping PID=%d should have been killed", pid2)
	}
}
