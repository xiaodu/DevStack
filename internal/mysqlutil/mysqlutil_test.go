package mysqlutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionLessThan(t *testing.T) {
	cases := []struct {
		v    string
		maj  int
		min  int
		want bool
	}{
		{"5.6.51", 5, 7, true},
		{"5.5.62", 5, 7, true},
		{"5.7.44", 5, 7, false},
		{"8.0.39", 5, 7, false},
		{"8.4.3", 5, 7, false},
		{"bad", 5, 7, false},
	}
	for _, c := range cases {
		if got := versionLessThan(c.v, c.maj, c.min); got != c.want {
			t.Errorf("versionLessThan(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}

// TestInitializeDataDir_AlreadyInitializedSkips：数据目录已有 mysql/ 系统库时直接返回 nil，
// 不会去动 mysqld.exe（这里放一个会失败的假 exe 来证明没被执行）。
func TestInitializeDataDir_AlreadyInitializedSkips(t *testing.T) {
	dir := t.TempDir()
	mysqlDir := filepath.Join(dir, "programs", "mysql", "5.7.44")
	dataDir := filepath.Join(dir, "data", "mysql", "5.7.44")
	if err := os.MkdirAll(filepath.Join(mysqlDir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	// 假 mysqld：真被执行会报错，用于断言"已初始化则跳过"
	if err := os.WriteFile(filepath.Join(mysqlDir, "bin", "mysqld.exe"), []byte("not-real"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "mysql"), 0755); err != nil {
		t.Fatal(err)
	}
	// 真正的"已初始化"标志：系统表 mysql.user 存在
	if err := os.WriteFile(filepath.Join(dataDir, "mysql", "user.frm"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := InitializeDataDir(Config{MySQLDir: mysqlDir, DataDir: dataDir, Version: "5.7.44"}); err != nil {
		t.Fatalf("已初始化应跳过，却返回错误: %v", err)
	}
}

// TestInitializeDataDir_PartialDirGivesActionableError：目录里有残留但缺系统表时，
// 返回"数据目录不完整"可操作错误，而不是静默跳过或以迷惑错误启动。
func TestInitializeDataDir_PartialDirGivesActionableError(t *testing.T) {
	dir := t.TempDir()
	mysqlDir := filepath.Join(dir, "programs", "mysql", "5.7.44")
	dataDir := filepath.Join(dir, "data", "mysql", "5.7.44")
	if err := os.MkdirAll(filepath.Join(mysqlDir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	// 半套初始化：有 mysql/ 目录和个别表，但没有 user.frm
	if err := os.MkdirAll(filepath.Join(dataDir, "mysql"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "ibdata1"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	err := InitializeDataDir(Config{MySQLDir: mysqlDir, DataDir: dataDir, Version: "5.7.44"})
	if err == nil {
		t.Fatal("残缺数据目录应返回错误")
	}
	if !strings.Contains(err.Error(), "不完整") {
		t.Errorf("应提示数据目录不完整，实际: %v", err)
	}
}

// TestInitializeDataDir_LegacyMissingInstallerGivesActionableError：
// MySQL <5.7 且发行包不含 mysql_install_db.exe 时，返回可操作错误而非崩溃日志。
func TestInitializeDataDir_LegacyMissingInstallerGivesActionableError(t *testing.T) {
	dir := t.TempDir()
	mysqlDir := filepath.Join(dir, "programs", "mysql", "5.6.51")
	dataDir := filepath.Join(dir, "data", "mysql", "5.6.51")
	if err := os.MkdirAll(filepath.Join(mysqlDir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mysqlDir, "bin", "mysqld.exe"), []byte("not-real"), 0755); err != nil {
		t.Fatal(err)
	}
	err := InitializeDataDir(Config{MySQLDir: mysqlDir, DataDir: dataDir, Version: "5.6.51"})
	if err == nil {
		t.Fatal("5.6 缺 mysql_install_db.exe 应返回错误")
	}
	if !strings.Contains(err.Error(), "mysql_install_db.exe") || !strings.Contains(err.Error(), "5.7") {
		t.Errorf("错误信息应可操作（提到 mysql_install_db.exe 与改用 5.7+），实际: %v", err)
	}
	// 数据目录应已被创建（MkdirAll 先行）
	if fi, serr := os.Stat(dataDir); serr != nil || !fi.IsDir() {
		t.Errorf("应预先创建数据目录 %s", dataDir)
	}
}
