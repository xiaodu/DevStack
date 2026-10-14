package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/xiaodu/DevStack/internal/atomicfile"
)

type StateStore struct {
	mu       sync.RWMutex
	dataDir  string
	filePath string
}

func NewStateStore(dataDir string) *StateStore {
	return &StateStore{
		dataDir:  dataDir,
		filePath: filepath.Join(dataDir, "environment.json"),
	}
}

func (ss *StateStore) Load() (EnvironmentState, error) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	var env EnvironmentState
	if _, err := os.Stat(ss.filePath); os.IsNotExist(err) {
		return env, nil
	}

	data, err := os.ReadFile(ss.filePath)
	if err != nil {
		return env, fmt.Errorf("读取环境状态文件失败: %w", err)
	}

	if err := json.Unmarshal(data, &env); err != nil {
		return env, fmt.Errorf("解析环境状态文件失败: %w", err)
	}

	return env, nil
}

func (ss *StateStore) Save(env EnvironmentState) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.saveJSON(env)
}

func (ss *StateStore) SaveActive(activeID string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	// 读-改-写：只更新 activeEnvironmentId 一个字段。
	// 不能像旧实现那样用单字段 map 整体覆盖文件，否则会把 Save()/回滚快照
	// 写入的 services/projects/runtimes 全部抹掉。
	var env EnvironmentState
	if _, err := os.Stat(ss.filePath); err == nil {
		if data, err := os.ReadFile(ss.filePath); err == nil {
			if err := json.Unmarshal(data, &env); err != nil {
				return fmt.Errorf("解析环境状态文件失败: %w", err)
			}
		} else {
			return fmt.Errorf("读取环境状态文件失败: %w", err)
		}
	}
	env.ActiveEnvironmentID = activeID
	return ss.saveJSON(env)
}

func (ss *StateStore) saveJSON(v interface{}) error {
	if err := os.MkdirAll(ss.dataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化数据失败: %w", err)
	}
	if err := atomicfile.WriteFile(ss.filePath, data, 0644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}
