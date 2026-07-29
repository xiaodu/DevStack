package resource

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type PathManager struct {
	mu        sync.RWMutex
	baseDir   string
	allocated map[string]string
}

func NewPathManager(baseDir string) *PathManager {
	return &PathManager{
		baseDir:   baseDir,
		allocated: make(map[string]string),
	}
}

func (pm *PathManager) Allocate(scope, name string) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	key := fmt.Sprintf("%s/%s", scope, name)
	if existing, ok := pm.allocated[key]; ok {
		return existing, nil
	}

	path := filepath.Join(pm.baseDir, scope, name)
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("创建路径失败 %s: %w", path, err)
	}

	pm.allocated[key] = path
	return path, nil
}

func (pm *PathManager) Release(scope, name string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	key := fmt.Sprintf("%s/%s", scope, name)
	delete(pm.allocated, key)
}

func (pm *PathManager) Get(scope, name string) (string, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	key := fmt.Sprintf("%s/%s", scope, name)
	path, ok := pm.allocated[key]
	return path, ok
}

func (pm *PathManager) Resolve(scope, name string) string {
	return filepath.Join(pm.baseDir, scope, name)
}

func (pm *PathManager) ListByScope(scope string) map[string]string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	result := make(map[string]string)
	prefix := scope + "/"
	for key, path := range pm.allocated {
		if strings.HasPrefix(key, prefix) {
			result[strings.TrimPrefix(key, prefix)] = path
		}
	}
	return result
}
