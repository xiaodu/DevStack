package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/tx"
)

type SnapshotManager struct {
	baseDir string
}

func NewSnapshotManager(baseDir string) *SnapshotManager {
	return &SnapshotManager{
		baseDir: filepath.Join(baseDir, "snapshots"),
	}
}

func (sm *SnapshotManager) TakeSnapshot(ctx context.Context, env EnvironmentState) (string, error) {
	if err := os.MkdirAll(sm.baseDir, 0755); err != nil {
		return "", fmt.Errorf("创建快照目录失败: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	snapshotID := fmt.Sprintf("snapshot-%s", timestamp)
	snapshotPath := filepath.Join(sm.baseDir, snapshotID+".json")

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化环境失败: %w", err)
	}

	if err := os.WriteFile(snapshotPath, data, 0644); err != nil {
		return "", fmt.Errorf("写入快照文件失败: %w", err)
	}

	sm.cleanupOldSnapshots()

	return snapshotID, nil
}

func (sm *SnapshotManager) LoadSnapshot(ctx context.Context, snapshotID string) (EnvironmentState, error) {
	snapshotPath := filepath.Join(sm.baseDir, snapshotID+".json")
	if !sm.exists(snapshotPath) {
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
	snapshotPath := filepath.Join(sm.baseDir, snapshotID+".json")
	if err := os.Remove(snapshotPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除快照失败: %w", err)
	}
	return nil
}

func (sm *SnapshotManager) CreateSnapshotStep(env EnvironmentState) (tx.StepFunc, tx.StepFunc) {
	return func(ctx context.Context) error {
		_, err := sm.TakeSnapshot(ctx, env)
		return err
	}, func(ctx context.Context) error {
		return nil
	}
}

func (sm *SnapshotManager) RestoreLastSnapshot() error {
	snapshots, err := sm.ListSnapshots(context.Background())
	if err != nil || len(snapshots) == 0 {
		return nil
	}
	last := snapshots[len(snapshots)-1]
	_, err = sm.LoadSnapshot(context.Background(), last)
	return err
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