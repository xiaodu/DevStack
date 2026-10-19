package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	return sm.listLocked()
}

// listLocked 列出快照标识。**调用方必须已持有 sm.mu**。
// Go 的 sync.Mutex 不可重入，持锁路径上一律走这些 *Locked 版本，
// 否则会自我死锁（TakeSnapshot → cleanupOldSnapshots → ListSnapshots 就是这么卡的）。
func (sm *SnapshotManager) listLocked() ([]string, error) {
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

	// 按创建先后排序：时间戳前缀 + 尾部计数器数值。
	// 不能按文件名字典序——同秒内 -10 会排在 -2 前面，导致恢复/清理选错快照
	sort.Slice(snapshots, func(i, j int) bool {
		ti, ci := parseSnapshotKey(snapshots[i])
		tj, cj := parseSnapshotKey(snapshots[j])
		if ti != tj {
			return ti < tj
		}
		return ci < cj
	})

	return snapshots, nil
}

// parseSnapshotKey 解析 "snapshot-20060102-150405-<n>" 的排序键。
// 无法解析的（历史遗留/外部文件）排最前，行为与旧字典序基本一致。
func parseSnapshotKey(id string) (string, int) {
	parts := strings.Split(id, "-")
	if len(parts) < 3 || parts[0] != "snapshot" {
		return id, -1
	}
	ts := parts[1] + "-" + parts[2]
	n := 0
	if len(parts) >= 4 {
		if v, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			n = v
		}
	}
	return ts, n
}

func (sm *SnapshotManager) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	if !validSnapshotID(snapshotID) {
		return fmt.Errorf("非法的快照标识: %s", snapshotID)
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.removeLocked(snapshotID)
}

// removeLocked 删除快照文件。**调用方必须已持有 sm.mu**。
func (sm *SnapshotManager) removeLocked(snapshotID string) error {
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

// cleanupOldSnapshots 保留最近 maxSnapshots 个快照。**调用方必须已持有 sm.mu**。
func (sm *SnapshotManager) cleanupOldSnapshots() {
	const maxSnapshots = 10

	snapshots, err := sm.listLocked()
	if err != nil {
		return
	}

	if len(snapshots) <= maxSnapshots {
		return
	}

	for i := 0; i < len(snapshots)-maxSnapshots; i++ {
		if !validSnapshotID(snapshots[i]) {
			// 外部/历史遗留文件不参与清理，避免按意外的标识删东西
			continue
		}
		_ = sm.removeLocked(snapshots[i])
	}
}

func (sm *SnapshotManager) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
