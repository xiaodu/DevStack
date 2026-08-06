package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaodu/DevStack/internal/environment"
)

// probe: SaveActive 写入的 activeEnvironmentId 必须能被读回（v4 文档：系统只保存这一条运行时信息）
func TestStateStoreSaveActiveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ss := NewStateStore(dir)
	const activeID = "builtin-lnmp-php74"
	if err := ss.SaveActive(activeID); err != nil {
		t.Fatalf("SaveActive: %v", err)
	}

	// 1) 文件层面：activeEnvironmentId 确实落盘
	path := filepath.Join(dir, "environment.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var raw struct {
		ActiveEnvironmentID string `json:"activeEnvironmentId"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw.ActiveEnvironmentID != activeID {
		t.Errorf("文件未写入 activeEnvironmentId, got %q want %q", raw.ActiveEnvironmentID, activeID)
	}

	// 2) API 层面：Load() 必须能读回同一个 ID（契约：持久化->读取 对称）
	state, err := ss.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.ActiveEnvironmentID != activeID {
		t.Errorf("SaveActive 后 Load 无法读回 activeEnvironmentId, got %q want %q", state.ActiveEnvironmentID, activeID)
	}
}

// probe: 激活后必须持久化 activeEnvironmentId（v4 文档 Activate 伪代码：activeEnvironmentId ← envId）
func TestActivatePersistsActiveEnvironment(t *testing.T) {
	dir := t.TempDir()
	ss := NewStateStore(dir)
	r := &ReconcilerImpl{stateStore: ss}

	target := environment.Environment{ID: "builtin-modern-php8"}
	old := environment.Environment{}

	if err := r.Activate(old, target); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	path := filepath.Join(dir, "environment.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var raw struct {
		ActiveEnvironmentID string `json:"activeEnvironmentId"`
	}
	json.Unmarshal(data, &raw)
	if raw.ActiveEnvironmentID != target.ID {
		t.Errorf("Activate 后未持久化 activeEnvironmentId, got %q want %q (oldEnv=%q)", raw.ActiveEnvironmentID, target.ID, old.ID)
	}
}

// probe: 停用后 activeEnvironmentId 必须为 null/空
func TestDeactivateClearsActiveEnvironment(t *testing.T) {
	dir := t.TempDir()
	ss := NewStateStore(dir)
	r := &ReconcilerImpl{stateStore: ss}

	if err := ss.SaveActive("builtin-lnmp-php74"); err != nil {
		t.Fatalf("SaveActive: %v", err)
	}
	if err := r.Deactivate(environment.Environment{ID: "builtin-lnmp-php74"}); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	path := filepath.Join(dir, "environment.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var raw struct {
		ActiveEnvironmentID string `json:"activeEnvironmentId"`
	}
	json.Unmarshal(data, &raw)
	if raw.ActiveEnvironmentID != "" {
		t.Errorf("Deactivate 后 activeEnvironmentId 应为空, got %q", raw.ActiveEnvironmentID)
	}
}
