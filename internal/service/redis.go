package service

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/resource"
)

type RedisHandler struct{}

func (h *RedisHandler) Configure(s *ServiceInstance, cfg *config.Config, resMgr *resource.Manager) error {
	installDir := cfg.Get().InstallDir
	redisDir := filepath.Join(installDir, "programs", "redis", s.Info.Version)

	redisConf := filepath.Join(redisDir, "redis.conf")
	if fileExists(redisConf) {
		return nil
	}

	conf := fmt.Sprintf(`port %d
bind 127.0.0.1
daemonize no
loglevel notice
logfile "redis.log"
dir "%s"
`, s.Info.Port, redisDir)
	return os.WriteFile(redisConf, []byte(conf), 0644)
}

func (h *RedisHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	installDir := cfg.Get().InstallDir
	confPath := filepath.Join(installDir, "programs", "redis", s.Info.Version, "redis.conf")
	if !fileExists(confPath) {
		return h.Configure(s, cfg, nil)
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}
	content := string(data)
	for k, v := range values {
		content = updateConfValue(content, k, v)
	}
	return os.WriteFile(confPath, []byte(content), 0644)
}


