package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/resource"
)

type MySQLHandler struct{}

func (h *MySQLHandler) Configure(s *ServiceInstance, cfg *config.Config, resMgr *resource.Manager) error {
	installDir := cfg.Get().InstallDir
	mysqlDir := filepath.Join(installDir, "programs", "mysql", s.Info.Version)
	dataDir := filepath.Join(installDir, "data", "mysql")

	myIni := filepath.Join(mysqlDir, "my.ini")
	if !fileExists(myIni) {
		ini := fmt.Sprintf(`[mysqld]
basedir=%s
datadir=%s
port=%d
bind-address=127.0.0.1
skip-networking=0
log-error=error.log
general_log=0

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

func (h *MySQLHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	installDir := cfg.Get().InstallDir
	mysqlDir := filepath.Join(installDir, "programs", "mysql", s.Info.Version)
	dataDir := filepath.Join(installDir, "data", "mysql")

	settings := cfg.Get()
	mc := settings.MySQLConfig
	changed := false

	if v, ok := values["innodbBufferPoolSize"]; ok {
		mc.InnoDBBufferPoolSize = v
		changed = true
	}
	if v, ok := values["innodbLogFileSize"]; ok {
		mc.InnoDBLogFileSize = v
		changed = true
	}
	if v, ok := values["innodbFlushLogAtTrxCommit"]; ok {
		var n int
		fmt.Sscanf(v, "%d", &n)
		mc.InnoDBFlushLogAtTrxCommit = n
		changed = true
	}
	if v, ok := values["maxConnections"]; ok {
		var n int
		fmt.Sscanf(v, "%d", &n)
		mc.MaxConnections = n
		changed = true
	}
	if v, ok := values["tmpTableSize"]; ok {
		mc.TmpTableSize = v
		changed = true
	}
	if v, ok := values["maxHeapTableSize"]; ok {
		mc.MaxHeapTableSize = v
		changed = true
	}
	if v, ok := values["threadCacheSize"]; ok {
		var n int
		fmt.Sscanf(v, "%d", &n)
		mc.ThreadCacheSize = n
		changed = true
	}
	if v, ok := values["tableOpenCache"]; ok {
		var n int
		fmt.Sscanf(v, "%d", &n)
		mc.TableOpenCache = n
		changed = true
	}
	if v, ok := values["bindAddress"]; ok {
		mc.BindAddress = v
		changed = true
	}
	if v, ok := values["port"]; ok {
		var n int
		fmt.Sscanf(v, "%d", &n)
		mc.Port = n
		changed = true
	}

	if changed {
		settings.MySQLConfig = mc
		if err := cfg.Update(settings); err != nil {
			return err
		}
	}

	myIni := filepath.Join(mysqlDir, "my.ini")
	ini := fmt.Sprintf(`[mysqld]
basedir=%s
datadir=%s
port=%d
bind-address=%s
skip-networking=0
log-error=error.log
general_log=0

innodb_buffer_pool_size = %s
innodb_log_file_size = %s
innodb_flush_log_at_trx_commit = %d
max_connections = %d
tmp_table_size = %s
max_heap_table_size = %s
thread_cache_size = %d
table_open_cache = %d
`, mysqlDir, dataDir, mc.Port, mc.BindAddress,
		mc.InnoDBBufferPoolSize, mc.InnoDBLogFileSize, mc.InnoDBFlushLogAtTrxCommit,
		mc.MaxConnections, mc.TmpTableSize, mc.MaxHeapTableSize,
		mc.ThreadCacheSize, mc.TableOpenCache)

	if err := os.WriteFile(myIni, []byte(ini), 0644); err != nil {
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
	mysqlDir := filepath.Join(installDir, "programs", "mysql", s.Info.Version)
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


