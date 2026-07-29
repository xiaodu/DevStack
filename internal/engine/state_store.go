package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type StateStore struct {
	dataDir string
	filePath string
}

func NewStateStore(dataDir string) *StateStore {
	return &StateStore{
		dataDir: dataDir,
		filePath: filepath.Join(dataDir, "environment.json"),
	}
}

func (ss *StateStore) Load() (EnvironmentState, error) {
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
	if err := os.MkdirAll(ss.dataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化环境状态失败: %w", err)
	}

	if err := os.WriteFile(ss.filePath, data, 0644); err != nil {
		return fmt.Errorf("写入环境状态文件失败: %w", err)
	}

	return nil
}
