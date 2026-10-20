package engine

import (
	"context"
	"testing"
	"time"
)

// 回归：TakeSnapshot 持有 sm.mu 后调用 cleanupOldSnapshots，而后者曾走公开的
// ListSnapshots/DeleteSnapshot（两者都会再加同一把不可重入的互斥锁），
// 结果第一次保存快照就永久自死锁。这里同时覆盖"超过保留数会清理旧快照"。
func TestTakeSnapshotDoesNotDeadlockAndPrunes(t *testing.T) {
	sm := NewSnapshotManager(t.TempDir())

	done := make(chan error, 1)
	go func() {
		for i := 0; i < maxSnapshotProbe; i++ {
			if _, err := sm.TakeSnapshot(context.Background(), EnvironmentState{
				ActiveEnvironmentID: "env-a",
			}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("TakeSnapshot 返回错误: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("TakeSnapshot 卡死（持锁后再加锁的自死锁回归）")
	}

	ids, err := sm.ListSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListSnapshots 失败: %v", err)
	}
	if len(ids) != keepSnapshots {
		t.Fatalf("应只保留 %d 个快照，实际 %d 个", keepSnapshots, len(ids))
	}

	// 最新的必须还在，且能被读出
	last := ids[len(ids)-1]
	state, err := sm.LoadSnapshot(context.Background(), last)
	if err != nil {
		t.Fatalf("LoadSnapshot(%s) 失败: %v", last, err)
	}
	if state.ActiveEnvironmentID != "env-a" {
		t.Fatalf("快照内容不符: %+v", state)
	}
}

// 回归：空快照列表曾返回 nil，经 encoding/json 序列化成 null，
// 前端按数组访问 length 抛错后页面永远停在“加载中”。
func TestListSnapshotsEmptyReturnsNonNilSlice(t *testing.T) {
	sm := NewSnapshotManager(t.TempDir())
	got, err := sm.ListSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListSnapshots 失败: %v", err)
	}
	if got == nil {
		t.Fatal("空列表必须返回空切片而不是 nil")
	}
	if len(got) != 0 {
		t.Fatalf("预期空列表，实际 %d 项", len(got))
	}
}

const (
	maxSnapshotProbe = 12
	keepSnapshots    = 10
)
