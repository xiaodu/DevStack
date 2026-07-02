package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type MySQLHandler struct{}

func (h *MySQLHandler) Configure(s *ServiceInstance, cfg *config.Config) error {
	installDir := cfg.Get().InstallDir
	mysqlDir := filepath.Join(installDir, "mysql", s.Info.Version)
	dataDir := filepath.Join(mysqlDir, "data")

	myIni := filepath.Join(mysqlDir, "my.ini")
	if !fileExists(myIni) {
		ini := fmt.Sprintf(`[mysqld]
basedir=%s
datadir=%s
port=%d
bind-address=127.0.0.1
skip-networking=0
`, mysqlDir, dataDir, s.Info.Port)
		if err := os.WriteFile(myIni, []byte(ini), 0644); err != nil {
			return err
		}
	}

	if !dirExists(dataDir) {
		mysqld := filepath.Join(mysqlDir, "bin", "mysqld.exe")
		if fileExists(mysqld) {
			cmd := exec.Command(mysqld, fmt.Sprintf("--defaults-file=%s", myIni), "--initialize-insecure")
			cmd.Dir = mysqlDir
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("初始化 MySQL 数据目录失败: %w\n输出: %s", err, string(out))
			}
		}
	}
	return nil
}

func (h *MySQLHandler) Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error {
	installDir := cfg.Get().InstallDir
	mysqlDir := filepath.Join(installDir, "mysql", s.Info.Version)
	mysqld := filepath.Join(mysqlDir, "bin", "mysqld.exe")

	if !fileExists(mysqld) {
		return fmt.Errorf("MySQL 可执行文件未找到: %s", mysqld)
	}

	dataDir := filepath.Join(mysqlDir, "data")
	os.MkdirAll(dataDir, 0755)

	myIni := filepath.Join(mysqlDir, "my.ini")
	if !fileExists(myIni) {
		ini := fmt.Sprintf(`[mysqld]
basedir=%s
datadir=%s
port=%d
bind-address=127.0.0.1
skip-networking=0
`, mysqlDir, dataDir, s.Info.Port)
		os.WriteFile(myIni, []byte(ini), 0644)
	}

	s.Info.Status = core.StatusStarting
	args := []string{
		fmt.Sprintf("--defaults-file=%s", myIni),
		"--console",
	}

	env := []string{fmt.Sprintf("MYSQL_HOME=%s", mysqlDir)}

	pid, err := pm.Start("mysql", mysqld, args, env, mysqlDir)
	if err != nil {
		s.Info.Status = core.StatusError
		s.Info.Error = err.Error()
		return err
	}

	s.process = &execWrapper{
		pid:     pid,
		cmd:     mysqld,
		started: now(),
	}
	s.Info.Status = core.StatusRunning
	s.Info.PID = pid
	return nil
}

func (h *MySQLHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	installDir := cfg.Get().InstallDir
	myIni := filepath.Join(installDir, "mysql", s.Info.Version, "my.ini")
	if !fileExists(myIni) {
		return h.Configure(s, cfg)
	}
	data, err := os.ReadFile(myIni)
	if err != nil {
		return err
	}
	content := string(data)
	for k, v := range values {
		content = updateIniValue(content, k, v)
	}
	return os.WriteFile(myIni, []byte(content), 0644)
}

func (h *MySQLHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
	if s.process == nil || s.process.pid <= 0 {
		return nil
	}
	s.Info.Status = core.StatusStopping
	if err := pm.Stop(s.process.pid); err != nil {
		s.Info.Status = core.StatusError
		return err
	}
	s.process = nil
	s.Info.Status = core.StatusStopped
	s.Info.PID = 0
	return nil
}
