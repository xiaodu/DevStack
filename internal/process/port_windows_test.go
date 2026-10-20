package process

import (
	"net"
	"os"
	"testing"
	"time"
)

// 回归锚点：端口监听的真相源已从 netstat 切到 GetExtendedTcpTable。
// 本进程自己 listen 一个端口，API 扫描必须能报告当前 PID——
// 若实现回退到 netstat 且 netstat 缺失/受限，此用例同样能暴露问题。
func TestListeningPortsViaAPI_FindsSelf(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	InvalidatePortCache()
	pids := FindPIDsByPort(port)
	if len(pids) == 0 {
		t.Fatalf("端口 %d 正在被本进程监听，API 扫描却报告空（真相源失效）", port)
	}
	found := false
	for _, pid := range pids {
		if pid == os.Getpid() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("端口 %d 的监听者应包含本进程 %d，实际 %v", port, os.Getpid(), pids)
	}
}

// 缓存失效语义：InvalidatePortCache 之后的首次查询必须重新扫描（不能只换时间戳）。
// 停止复核依赖这一点；若缓存清了个寂寞，强杀后仍会读到“还在监听”的旧快照。
func TestInvalidatePortCacheForcesRescan(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	InvalidatePortCache()
	if len(FindPIDsByPort(port)) == 0 {
		ln.Close()
		t.Fatalf("端口 %d 未被扫出", port)
	}

	ln.Close()
	// 等内核释放端口（TIME_WAIT 不影响 LISTEN 表）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		InvalidatePortCache()
		if len(FindPIDsByPort(port)) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("关闭监听后作废缓存重扫，端口 %d 仍报告有监听者", port)
}
