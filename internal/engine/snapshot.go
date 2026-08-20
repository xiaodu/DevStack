package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaodu/DevStack/internal/atomicfile"
	"github.com/xiaodu/DevStack/internal/tx"
)

var snapshotCounter atomic.Uint64

type SnapshotManager struct {
	mu      sync.Mutex
	baseDir string
}

func NewSnapshotManager(baseDir string) *SnapshotManager {
	return &SnapshotManager{
		baseDir: filepath.Join(baseDir, "snapshots"),
	}
}

func (sm *SnapshotManager) TakeSnapshot(ctx context.Context, env EnvironmentState) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if err := os.MkdirAll(sm.baseDir, 0755); err != nil {
		return "", fmt.Errorf("创建快照目录失败: %w", err)
	}

	counter := snapshotCounter.Add(1)
	timestamp := time.Now().Format("20060102-150405")
	snapshotID := fmt.Sprintf("snapshot-%s-%d", timestamp, counter)
	snapshotPath := filepath.Join(sm.baseDir, snapshotID+".json")

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化环境失败: %w", err)
	}

	if err := atomicfile.WriteFile(snapshotPath, data, 0644); err != nil {
		return "", fmt.Errorf("写入快照文件失败: %w", err)
	}

	sm.cleanupOldSnapshots()

	return snapshotID, nil
}

func (sm *SnapshotManager) LoadSnapshot(ctx context.Context, snapshotID string) (EnvironmentState, error) {
	if !validSnapshotID(snapshotID) {
		return EnvironmentState{}, fmt.Errorf("非法的快照标识: %s", snapshotID)
	}
	sm.mu.Lock()
	snapshotPath := filepath.Join(sm.baseDir, snapshotID+".json")
	exists := sm.exists(snapshotPath)
	sm.mu.Unlock()
	if !exists {
		return EnvironmentState{}, fmt.Errorf("快照 %s 不存在", snapshotID)
	}

	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return EnvironmentState{}, fmt.Errorf("读取快照文件失败: %w", err)
	}

	var env EnvironmentState
	if err := json.Unmarshal(data, &env); err != nil {
		return EnvironmentState{}, fmt.Errorf("反序列化快照失败: %w", err)
	}

	return env, nil
}

func (sm *SnapshotManager) ListSnapshots(ctx context.Context) ([]string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.exists(sm.baseDir) {
		return nil, nil
	}

	entries, err := os.ReadDir(sm.baseDir)
	if err != nil {
		return nil, fmt.Errorf("读取快照目录失败: %w", err)
	}

	var snapshots []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) > 5 && name[len(name)-5:] == ".json" {
			snapshots = append(snapshots, name[:len(name)-5])
		}
	}

	return snapshots, nil
}

func (sm *SnapshotManager) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	if !validSnapshotID(snapshotID) {
		return fmt.Errorf("非法的快照标识: %s", snapshotID)
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	snapshotPath := filepath.Join(sm.baseDir, snapshotID+".json")
	if err := os.Remove(snapshotPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除快照失败: %w", err)
	}
	return nil
}

// validSnapshotID 仅允许形如 snapshot-20060102-150405-N 的标识，阻止路径穿越。
func validSnapshotID(snapshotID string) bool {
	if !strings.HasPrefix(snapshotID, "snapshot-") {
		return false
	}
	rest := snapshotID[len("snapshot-"):]
	if len(rest) < 15 {
		return false
	}
	for i := 0; i < 15; i++ {
		r := rest[i]
		if (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	if len(rest) > 15 {
		if rest[15] != '-' {
			return false
		}
		for _, r := range rest[16:] {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func (sm *SnapshotManager) CreateSnapshotStep(env EnvironmentState) (tx.StepFunc, tx.StepFunc) {
	return func(ctx context.Context) error {
		_, err := sm.TakeSnapshot(ctx, env)
		return err
	}, func(ctx context.Context) error {
		return nil
	}
}

func (sm *SnapshotManager) RestoreLastSnapshot() (EnvironmentState, error) {
	snapshots, err := sm.ListSnapshots(context.Background())
	if err != nil || len(snapshots) == 0 {
		return EnvironmentState{}, err
	}
	last := snapshots[len(snapshots)-1]
	return sm.LoadSnapshot(context.Background(), last)
}

func (sm *SnapshotManager) cleanupOldSnapshots() {
	const maxSnapshots = 10

	snapshots, err := sm.ListSnapshots(context.Background())
	if err != nil {
		return
	}

	if len(snapshots) <= maxSnapshots {
		return
	}

	for i := 0; i < len(snapshots)-maxSnapshots; i++ {
		sm.DeleteSnapshot(context.Background(), snapshots[i])
	}
}

func (sm *SnapshotManager) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}