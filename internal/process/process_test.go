package process

import (
	"strings"
	"testing"
	"time"
)

// probe: 滚动缓冲裁剪必须按完整行进行，不允许切断行首。
func TestAppendTrimmed_KeepsWholeLines(t *testing.T) {
	cap := 32
	var buf []byte
	// 写入足够多的短行触发裁剪
	for i := 0; i < 100; i++ {
		buf = appendTrimmed(buf, []byte("hello-world-line\n"), cap)
	}
	if len(buf) > cap {
		t.Fatalf("裁剪后仍超上限: len=%d cap=%d", len(buf), cap)
	}
	if !strings.HasPrefix(string(buf), "hello-world-line\n") {
		t.Fatalf("裁剪后首行不完整: %q", buf)
	}
	lines := strings.Split(strings.TrimSuffix(string(buf), "\n"), "\n")
	for _, ln := range lines {
		if ln != "hello-world-line" {
			t.Fatalf("存在被切断的半行: %q", ln)
		}
	}
}

// probe: 单行超长（无换行）时允许硬截断，不 panic。
func TestAppendTrimmed_SingleHugeLine(t *testing.T) {
	cap := 16
	var buf []byte
	buf = appendTrimmed(buf, []byte(strings.Repeat("x", 1000)), cap)
	if len(buf) > cap {
		t.Fatalf("超长单行未裁剪: len=%d cap=%d", len(buf), cap)
	}
}

// probe: P1-7 — 进程退出后必须清理 outputs，避免高频重启场景内存无界增长。
func TestManager_OutputCleanedOnExit(t *testing.T) {
	m := NewManager()
	pid, err := m.Start("test-exit", "cmd", []string{"/c", "exit", "0"}, nil, t.TempDir())
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	<-m.WaitChan(pid)
	// close(exitCh) 发生在清理之前，需轮询等待 outputs 删除
	deadline := time.Now().Add(3 * time.Second)
	for m.GetOutput(pid) != nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := m.GetOutput(pid); got != nil {
		t.Fatalf("进程 %d 退出后 outputs 未清理", pid)
	}
}

// probe: 运行期间输出应被捕获。
func TestManager_CapturesOutputWhileRunning(t *testing.T) {
	m := NewManager()
	// 输出一段文本后保持存活 ~2s，给读取轮询留出窗口
	pid, err := m.Start("test-out", "cmd", []string{"/c", "echo hello-capture && ping -n 3 127.0.0.1 > nul"}, nil, t.TempDir())
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer m.Stop(pid)

	captured := false
	deadline := time.Now().Add(5 * time.Second)
	for !captured && time.Now().Before(deadline) {
		if out := m.GetOutput(pid); out != nil && strings.Contains(string(out.Stdout()), "hello-capture") {
			captured = true
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !captured {
		t.Fatalf("运行期间未捕获到输出")
	}
}

// probe: 并发 Start 多个进程并立即 Stop 不应 panic / data race。
func TestManager_ConcurrentStartStop(t *testing.T) {
	m := NewManager()
	const n = 8
	pids := make([]int, 0, n)
	for i := 0; i < n; i++ {
		pid, err := m.Start("test-conc", "cmd", []string{"/c", "exit", "0"}, nil, t.TempDir())
		if err != nil {
			t.Fatalf("启动失败: %v", err)
		}
		pids = append(pids, pid)
	}
	for _, pid := range pids {
		_ = m.Stop(pid) // 已退出时 Stop 返回错误属正常
	}
	for _, pid := range pids {
		<-m.WaitChan(pid)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		allCleaned := true
		for _, pid := range pids {
			if m.GetOutput(pid) != nil {
				allCleaned = false
				break
			}
		}
		if allCleaned {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("并发场景下存在未清理的 outputs")
}
