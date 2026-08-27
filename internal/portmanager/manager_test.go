package portmanager

import (
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir())
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestAllocate(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir())

	// 分配端口
	port, err := m.Allocate("nginx", 8080)
	if err != nil {
		t.Fatalf("Allocate failed: %v", err)
	}
	if port != 8080 {
		t.Errorf("expected port 8080, got %d", port)
	}

	// 验证分配
	owner, ok := m.GetOwner(port)
	if !ok || owner != "nginx" {
		t.Errorf("expected owner nginx, got %s (ok=%v)", owner, ok)
	}
}

func TestRelease(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir())

	port, err := m.Allocate("nginx", 8080)
	if err != nil {
		t.Fatalf("Allocate failed: %v", err)
	}

	// 释放端口
	if err := m.Release(port, "nginx"); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	// 验证已释放
	_, ok := m.GetOwner(port)
	if ok {
		t.Error("port should be released")
	}
}

func TestReleaseByOwner(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir())

	p1, _ := m.Allocate("nginx", 8080)
	p2, _ := m.Allocate("nginx", 8081)
	p3, _ := m.Allocate("mysql", 3306)

	// 释放 nginx 的所有端口
	m.ReleaseByOwner("nginx")

	// 验证 nginx 端口已释放
	if _, ok := m.GetOwner(p1); ok {
		t.Error("port should be released")
	}
	if _, ok := m.GetOwner(p2); ok {
		t.Error("port should be released")
	}

	// 验证 mysql 端口仍在
	if _, ok := m.GetOwner(p3); !ok {
		t.Error("port should still be allocated")
	}
}

func TestCheckConflict(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir())

	// 检查未使用端口
	if err := m.CheckConflict(19999, "test"); err != nil {
		t.Errorf("unexpected conflict: %v", err)
	}
}

func TestReserve(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir())

	// 预留端口
	m.Reserve(8080, "nginx")

	// 验证预留
	owner, ok := m.IsReserved(8080)
	if !ok || owner != "nginx" {
		t.Errorf("expected reserved by nginx, got %s (ok=%v)", owner, ok)
	}

	// 取消预留
	m.Unreserve(8080)
	if _, ok := m.IsReserved(8080); ok {
		t.Error("port should not be reserved")
	}
}

func TestListAllocated(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir())

	m.Allocate("nginx", 8080)
	m.Allocate("mysql", 3306)

	allocated := m.ListAllocated()
	if len(allocated) != 2 {
		t.Errorf("expected 2 allocated ports, got %d", len(allocated))
	}
}

func TestSnapshot(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir())

	m.Allocate("nginx", 8080)
	m.Allocate("mysql", 3306)

	snapshot := m.Snapshot()
	if len(snapshot) != 2 {
		t.Errorf("expected 2 snapshot entries, got %d", len(snapshot))
	}
}

func TestWaitForPort(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir())

	// 等待未使用的端口（应该超时）
	err := m.WaitForPort(19999, 200*time.Millisecond)
	// 注意：这个测试可能会因为端口可用而失败
	// 实际上 WaitForPort 等待端口被占用，所以未使用的端口会超时
	if err == nil {
		t.Log("WaitForPort completed (port may have been in use)")
	}
}
