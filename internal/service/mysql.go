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

type MySQLHandler struct {
	BaseHandler
}

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

innodb_buffer_pool_size = 256M
innodb_log_file_size = 64M
innodb_flush_log_at_trx_commit = 2
max_connections = 50
tmp_table_size = 64M
max_heap_table_size = 64M
thread_cache_size = 8
table_open_cache = 400
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

innodb_buffer_pool_size = 256M
innodb_log_file_size = 64M
innodb_flush_log_at_trx_commit = 2
max_connections = 50
tmp_table_size = 64M
max_heap_table_size = 64M
thread_cache_size = 8
table_open_cache = 400
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
	if err := os.WriteFile(myIni, []byte(content), 0644); err != nil {
		return err
	}

	if password, ok := values["password"]; ok && password != "" {
		if err := h.setPassword(s, cfg, password); err != nil {
			return fmt.Errorf("配置已保存，但设置密码失败: %w", err)
		}
	}
	return nil
}

func (h *MySQLHandler) setPassword(s *ServiceInstance, cfg *config.Config, newPassword string) error {
	installDir := cfg.Get().InstallDir
	mysqlDir := filepath.Join(installDir, "mysql", s.Info.Version)
	mysqlBin := filepath.Join(mysqlDir, "bin", "mysql.exe")

	if !fileExists(mysqlBin) {
		return fmt.Errorf("mysql 客户端未找到: %s", mysqlBin)
	}

	port := s.Info.Port
	if port == 0 {
		port = 3306
	}

	pid := process.FindPIDByPort(port)
	if pid <= 0 {
		return fmt.Errorf("MySQL 未运行，请先启动 MySQL 后再设置密码")
	}

	sql := buildSetPasswordSQL(s.Info.Version, newPassword)
	args := []string{
		fmt.Sprintf("--port=%d", port),
		"-u", "root",
		"-e", sql,
	}

	cmd := exec.Command(mysqlBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("设置密码失败 (请确保 MySQL 已启动且 root 当前无密码): %w\n%s", err, string(out))
	}
	return nil
}

func buildSetPasswordSQL(version, password string) string {
	if isMySQL8OrAbove(version) {
		return fmt.Sprintf("ALTER USER 'root'@'localhost' IDENTIFIED WITH mysql_native_password BY '%s';", password)
	}
	return fmt.Sprintf("SET PASSWORD FOR 'root'@'localhost' = PASSWORD('%s');", password)
}

func isMySQL8OrAbove(version string) bool {
	major := 0
	for i := 0; i < len(version); i++ {
		if version[i] >= '0' && version[i] <= '9' {
			major = major*10 + int(version[i]-'0')
		} else {
			break
		}
	}
	return major >= 8
}

func (h *MySQLHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
	if s.process == nil || s.process.pid <= 0 {
		return nil
	}
	s.Info.Status = core.StatusStopping

	binDir := filepath.Dir(s.process.cmd)
	mysqladmin := filepath.Join(binDir, "mysqladmin.exe")
	if fileExists(mysqladmin) {
		cmd := exec.Command(mysqladmin, "-u", "root", "shutdown")
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
