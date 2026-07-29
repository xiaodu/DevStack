package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/xiaodu/DevStack/internal/config"
)

const phpWorkerCount = 4

type PHPExtensionInfo struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type PHPVersionInfo struct {
	Version string `json:"version"`
	Port    int    `json:"port"`
}

type PHPRegistry struct {
	Versions []PHPVersionInfo `json:"versions"`
}

func getRegistryPath(cfg *config.Config) string {
	return filepath.Join(cfg.Get().InstallDir, "config", "php_registry.json")
}

func LoadPHPRegistry(cfg *config.Config) *PHPRegistry {
	reg := &PHPRegistry{Versions: make([]PHPVersionInfo, 0)}
	data, err := os.ReadFile(getRegistryPath(cfg))
	if err != nil {
		return reg
	}
	json.Unmarshal(data, reg)
	return reg
}

func SavePHPRegistry(cfg *config.Config, reg *PHPRegistry) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(getRegistryPath(cfg)), 0755)
	return os.WriteFile(getRegistryPath(cfg), data, 0644)
}

func GetPHPVersionPort(cfg *config.Config, version string) int {
	reg := LoadPHPRegistry(cfg)
	for _, v := range reg.Versions {
		if v.Version == version {
			return v.Port
		}
	}
	return 0
}

func RegisterPHPVersion(cfg *config.Config, version string, port int) {
	reg := LoadPHPRegistry(cfg)
	for i, v := range reg.Versions {
		if v.Version == version {
			reg.Versions[i].Port = port
			SavePHPRegistry(cfg, reg)
			return
		}
	}
	reg.Versions = append(reg.Versions, PHPVersionInfo{Version: version, Port: port})
	SavePHPRegistry(cfg, reg)
}

func AutoAssignPorts(cfg *config.Config, installDir string, portPool interface{ Allocate(owner string, preferred int) (int, error) }) {
	phpDir := filepath.Join(installDir, "programs", "php")
	entries, err := os.ReadDir(phpDir)
	if err != nil {
		return
	}

	reg := LoadPHPRegistry(cfg)
	existingPorts := make(map[int]bool)
	for _, v := range reg.Versions {
		if v.Port > 0 {
			existingPorts[v.Port] = true
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ver := entry.Name()
		found := false
		for _, v := range reg.Versions {
			if v.Version == ver {
				found = true
				break
			}
		}
		if !found {
			port, err := portPool.Allocate("php:"+ver, 0)
			if err != nil {
				continue
			}
			reg.Versions = append(reg.Versions, PHPVersionInfo{Version: ver, Port: port})
			existingPorts[port] = true
		}
	}
	SavePHPRegistry(cfg, reg)
}

func (m *Manager) GetExtensions(version string) ([]PHPExtensionInfo, error) {
	return getExtensionList(m.cfg, version)
}

func (m *Manager) SaveExtensions(version string, extensionNames []string) error {
	return saveExtensionList(m.cfg, version, extensionNames)
}

func (m *Manager) GetPHPRegistryVersions() []PHPVersionInfo {
	reg := LoadPHPRegistry(m.cfg)
	return reg.Versions
}

func (m *Manager) RefreshPHPRegistry() []PHPVersionInfo {
	installDir := m.cfg.Get().InstallDir
	if installDir != "" {
		AutoAssignPorts(m.cfg, installDir, m.resMgr.Ports())
	}
	reg := LoadPHPRegistry(m.cfg)
	return reg.Versions
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
