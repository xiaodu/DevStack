package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type RedisHandler struct {
	BaseHandler
}

func (h *RedisHandler) Configure(s *ServiceInstance, cfg *config.Config) error {
	installDir := cfg.Get().InstallDir
	redisDir := filepath.Join(installDir, "redis", s.Info.Version)

	redisConf := filepath.Join(redisDir, "redis.conf")
	if fileExists(redisConf) {
		return nil
	}

	conf := fmt.Sprintf(`port %d
bind 127.0.0.1
daemonize no
loglevel notice
logfile ""
dir "%s"
`, s.Info.Port, redisDir)
	return os.WriteFile(redisConf, []byte(conf), 0644)
}

func (h *RedisHandler) Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error {
	installDir := cfg.Get().InstallDir
	redisDir := filepath.Join(installDir, "redis", s.Info.Version)
	redisServer := filepath.Join(redisDir, "redis-server.exe")

	if !fileExists(redisServer) {
		redisServer = filepath.Join(redisDir, "src", "redis-server.exe")
	}
	if !fileExists(redisServer) {
		return fmt.Errorf("Redis 可执行文件未找到: %s", redisServer)
	}

	redisConf := filepath.Join(redisDir, "redis.conf")
	if !fileExists(redisConf) {
		conf := fmt.Sprintf(`port %d
bind 127.0.0.1
daemonize no
loglevel notice
logfile ""
dir "%s"
`, s.Info.Port, redisDir)
		os.WriteFile(redisConf, []byte(conf), 0644)
	}

	s.Info.Status = core.StatusStarting
	args := []string{redisConf}
	pid, err := pm.Start("redis", redisServer, args, nil, redisDir)
	if err != nil {
		s.Info.Status = core.StatusError
		s.Info.Error = err.Error()
		return err
	}

	s.process = &execWrapper{
		pid:     pid,
		cmd:     redisServer,
		started: time.Now(),
	}
	s.Info.Status = core.StatusRunning
	s.Info.PID = pid
	return nil
}

func (h *RedisHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	installDir := cfg.Get().InstallDir
	confPath := filepath.Join(installDir, "redis", s.Info.Version, "redis.conf")
	if !fileExists(confPath) {
		return h.Configure(s, cfg)
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

func (h *RedisHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
	if s.process == nil || s.process.pid <= 0 {
		return nil
	}
	s.Info.Status = core.StatusStopping

	binDir := filepath.Dir(s.process.cmd)
	redisCli := filepath.Join(binDir, "redis-cli.exe")
	if fileExists(redisCli) {
		cmd := exec.Command(redisCli, "-p", fmt.Sprintf("%d", s.Info.Port), "SHUTDOWN")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Run(); err == nil {
			s.process = nil
			s.Info.Status = core.StatusStopped
			s.Info.PID = 0
			return nil
		}
	}

	if err := pm.Stop(s.process.pid); err != nil {
		s.Info.Status = core.StatusError
		return err
	}
	s.process = nil
	s.Info.Status = core.StatusStopped
	s.Info.PID = 0
	return nil
}
