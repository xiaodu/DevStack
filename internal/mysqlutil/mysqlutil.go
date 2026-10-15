package mysqlutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/process"
)

// Config 描述一次 MySQL 数据目录的 root@127.0.0.1 账号引导参数。
type Config struct {
	MySQLDir string // programs/mysql/<version>
	DataDir  string
	Port     int
	Version  string // 如 "5.7.44" / "8.0.39"
	MyIni    string // 服务器配置文件（可为空，空则用 mysqld 编译默认值）
}

func (c Config) mysqlExe() string { return filepath.Join(c.MySQLDir, "bin", "mysqld.exe") }
func (c Config) mysqlBin() string { return filepath.Join(c.MySQLDir, "bin", "mysql.exe") }

// InitializeDataDir 初始化 MySQL 数据目录（创建系统表），幂等：已初始化则直接返回。
//
// 版本策略：
//   - >= 5.7：mysqld --initialize-insecure（创建无密码 root@localhost）。
//   - <  5.7（5.5/5.6）：--initialize-insecure 尚不存在，需 bin/mysql_install_db.exe；
//     而官方 ZIP 发行包通常只带 scripts/mysql_install_db.pl（需 perl，终端用户没有），
//     不含该 exe。此时返回可操作的错误，而不是让 mysqld 以普通服务模式启动后
//     抛出 "Can't change dir / Table 'mysql.plugin' doesn't exist" 这类误导性崩溃日志。
func InitializeDataDir(c Config) error {
	if err := os.MkdirAll(c.DataDir, 0755); err != nil {
		return fmt.Errorf("创建 MySQL 数据目录失败: %w", err)
	}
	if initialized(c.DataDir) {
		return nil
	}
	// 目录里有东西但没有系统表 → 上次 --initialize 中断/损坏（半套 mysql 库），
	// 此时直接启动会以 "Table 'mysql.user' doesn't exist" 之类的迷惑错误失败。
	// 不自动清空（可能含用户数据），给出可操作提示。
	if dirHasContent(c.DataDir) {
		return fmt.Errorf("MySQL 数据目录不完整（缺系统表，疑似上次初始化中断）：%s；如需重置请删除该目录后重试（会清空其中数据）", c.DataDir)
	}

	if versionLessThan(c.Version, 5, 7) {
		installer := filepath.Join(c.MySQLDir, "bin", "mysql_install_db.exe")
		if !fileExists(installer) {
			return fmt.Errorf(
				"MySQL %s 的 ZIP 发行包不含 mysql_install_db.exe，无法自动初始化数据目录；"+
					"请改用 MySQL 5.7+，或手动初始化 %s",
				c.Version, c.DataDir)
		}
		cmd := exec.Command(installer, "--datadir="+c.DataDir)
		cmd.Dir = c.MySQLDir
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("mysql_install_db 初始化数据目录失败: %v\n%s", err, string(out))
		}
		return nil
	}

	args := []string{"--initialize-insecure", "--datadir=" + c.DataDir}
	if c.MyIni != "" && fileExists(c.MyIni) {
		args = append([]string{"--defaults-file=" + c.MyIni}, args...)
	}
	// 可执行文件缺失（如未真正安装的 mock 场景）时静默跳过，与旧 service.Configure 行为一致；
	// 真实未安装的服务已被 versionInstalled 的 exe 判据挡在注册之外，不会走到这里。
	if !fileExists(c.mysqlExe()) {
		return nil
	}
	cmd := exec.Command(c.mysqlExe(), args...)
	cmd.Dir = c.MySQLDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("初始化 MySQL 数据目录失败: %v\n%s", err, string(out))
	}
	return nil
}

// initialized 判断数据目录是否已建好系统表。
// 仅凭 mysql/ 目录存在不够：中断的 --initialize 会留下半套系统表（缺 mysql.user 等），
// 之后启动会以 "Table 'mysql.user' doesn't exist" 之类的迷惑错误失败。
// 5.5/5.6/5.7 看 mysql/user.frm；8.x 用独立数据字典，看根级 mysql.ibd。
func initialized(dataDir string) bool {
	if fileExists(filepath.Join(dataDir, "mysql", "user.frm")) {
		return true
	}
	if fileExists(filepath.Join(dataDir, "mysql.ibd")) {
		return true
	}
	return false
}

// dirHasContent 判断目录是否非空（含任意文件/子目录）。
func dirHasContent(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}

// versionLessThan 解析 "5.6.51" 这类版本串并与 (maj,min) 比较；解析失败按不低于处理。
func versionLessThan(version string, maj, min int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	if major != maj {
		return major < maj
	}
	return minor < min
}

type probeResult int

const (
	probeOK probeResult = iota
	probeAuthDenied
	probeHostDenied
	probeUnreachable
	probeOther
)

// EnsureRootIPAccount 确保 root@127.0.0.1 账号存在。
//
// 背景：Windows 下客户端经 TCP 连 127.0.0.1 时，MySQL 靠反向解析把 IP 映射回
// localhost 才能匹配 root@localhost；解析失败时主机名就是字面 IP，于是报
// "Host '127.0.0.1' is not allowed to connect"（错误 1130）。所以
// --initialize-insecure 只建 root@localhost 是不够的，必须额外建 root@127.0.0.1。
//
// 探测逻辑：
//   - 连接成功或提示 Access denied（账号已存在，只是有密码）→ 直接返回，不改动任何数据；
//   - 提示 1130（账号缺失）或服务器未运行 → 以 --skip-grant-tables 启动临时实例创建账号。
//
// 返回 healed=true 表示期间停掉了端口上已有的本目录实例，调用方需负责重新启动正常实例。
func EnsureRootIPAccount(c Config) (healed bool, err error) {
	switch probe(c) {
	case probeOK, probeAuthDenied:
		return false, nil
	case probeHostDenied:
		// 账号缺失：需要临时实例，先停掉正在占端口的本目录实例
	case probeUnreachable, probeOther:
		// 服务器未运行：直接引导，无需停已有进程
	}

	if err := stopOwned(c); err != nil {
		return false, err
	}
	// 强杀后稍等，让 InnoDB 释放数据目录文件锁，避免引导实例与旧进程竞争
	time.Sleep(500 * time.Millisecond)

	boot, err := startBootstrap(c)
	if err != nil {
		return false, err
	}
	cleanup := func() {
		_ = stopProcess(boot)
	}

	if err := createRootIPAccount(c); err != nil {
		cleanup()
		return false, err
	}
	if err := stopProcess(boot); err != nil {
		return true, err
	}
	return true, nil
}

// probe 尝试以 root 无密码连接 127.0.0.1，判断账号状态。
func probe(c Config) probeResult {
	out, err := runSQLOutput(c, "SELECT 1;")
	if err == nil {
		return probeOK
	}
	s := string(out)
	switch {
	case strings.Contains(s, "1130"):
		return probeHostDenied
	case strings.Contains(s, "1045"), strings.Contains(s, "Access denied"):
		return probeAuthDenied
	case strings.Contains(s, "2003"), strings.Contains(s, "2002"), strings.Contains(s, "10061"):
		return probeUnreachable
	}
	return probeOther
}

// stopOwned 停掉端口上属于本数据目录的实例（防止端口冲突）。
func stopOwned(c Config) error {
	for i := 0; i < 20; i++ {
		pid := process.FindPIDByPort(c.Port)
		if pid <= 0 {
			return nil
		}
		if !ownedBy(pid, c) {
			return fmt.Errorf("端口 %d 被其它程序占用（PID %d），无法引导 MySQL root 账号", c.Port, pid)
		}
		if err := stopProcess(pid); err != nil {
			return err
		}
	}
	return fmt.Errorf("等待端口 %d 释放超时", c.Port)
}

// ownedBy 判断进程是否属于该 MySQL 安装目录。
func ownedBy(pid int, c Config) bool {
	p := process.GetExecutablePath(pid)
	return strings.HasPrefix(strings.ToLower(p), strings.ToLower(c.MySQLDir))
}

// stopProcess 强制终止进程并等待退出。
func stopProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
	for i := 0; i < 20; i++ {
		if !process.IsProcessRunning(pid) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("进程 %d 未能终止", pid)
}

// startBootstrap 以 --skip-grant-tables 启动临时实例并等待端口就绪。
func startBootstrap(c Config) (int, error) {
	args := []string{
		"--datadir=" + c.DataDir,
		"--port=" + strconv.Itoa(c.Port),
		"--skip-grant-tables",
	}
	if c.MyIni != "" && fileExists(c.MyIni) {
		args = append([]string{"--defaults-file=" + c.MyIni}, args...)
	}
	cmd := exec.Command(c.mysqlExe(), args...)
	cmd.Dir = c.MySQLDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("启动 MySQL 引导实例失败: %w", err)
	}
	spawned := cmd.Process.Pid
	for i := 0; i < 24; i++ {
		time.Sleep(500 * time.Millisecond)
		if p := process.FindPIDByPort(c.Port); p > 0 {
			return p, nil
		}
		if !process.IsProcessRunning(spawned) {
			_ = stopProcess(spawned)
			return 0, fmt.Errorf("MySQL 引导实例启动后即退出，请检查 %s 下的错误日志", c.MySQLDir)
		}
	}
	_ = stopProcess(spawned)
	return 0, fmt.Errorf("MySQL 引导实例未监听端口 %d", c.Port)
}

// createRootIPAccount 创建 root@127.0.0.1（克隆 root@localhost 行：同密码、同权限）。
//
// 引导实例以 --skip-grant-tables 运行时的两条限制决定了实现方式：
//  1. 账号管理语句（CREATE USER/GRANT）被禁止（ERROR 1290）；
//  2. FLUSH PRIVILEGES 会重新加载授权表并立即恢复访问控制，之后本函数的
//     新连接会被拒（ERROR 1130/1045），导致引导死循环。
//
// 因此只能对授权表做普通 DML：用临时表克隆 root@localhost 行并改写 Host，
// 不硬编码列结构，兼容 5.7 / 8.x。所有语句必须在同一次客户端调用中执行
// （同一会话内 TEMPORARY 表才可见）。新账号在实例重启加载授权表后生效；
// 引导实例随后被停止，正常实例启动时自然读到最新数据。
func createRootIPAccount(c Config) error {
	out, err := runSQLOutput(c, "SELECT COUNT(*) FROM mysql.user WHERE User='root' AND Host='127.0.0.1';")
	if err != nil {
		return fmt.Errorf("查询 root@127.0.0.1 失败: %w", err)
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(firstLine(out))); n > 0 {
		return nil
	}
	clone := "DELETE FROM mysql.user WHERE User='root' AND Host='127.0.0.1';" +
		"CREATE TEMPORARY TABLE mysql.devstack_tmp_user LIKE mysql.user;" +
		"INSERT INTO mysql.devstack_tmp_user SELECT * FROM mysql.user WHERE User='root' AND Host='localhost';" +
		"UPDATE mysql.devstack_tmp_user SET Host='127.0.0.1', authentication_string='', password_expired='N', account_locked='N';" +
		"INSERT INTO mysql.user SELECT * FROM mysql.devstack_tmp_user;" +
		"DROP TEMPORARY TABLE mysql.devstack_tmp_user;"
	if err := runSQL(c, clone); err != nil {
		return fmt.Errorf("创建 root@127.0.0.1 失败: %w", err)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runSQL(c Config, sql string) error {
	_, err := runSQLOutput(c, sql)
	return err
}

func runSQLOutput(c Config, sql string) (string, error) {
	args := []string{
		"--host=127.0.0.1",
		"--port=" + strconv.Itoa(c.Port),
		"-u", "root",
		"-e", sql,
	}
	cmd := exec.Command(c.mysqlBin(), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return string(out), fmt.Errorf("%w: %s", err, msg)
		}
	}
	return string(out), err
}