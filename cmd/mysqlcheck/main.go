package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/mysqlutil"
	"github.com/xiaodu/DevStack/internal/process"
)

func main() {
	cfg := mysqlutil.Config{
		MySQLDir: `E:\DevStack\programs\mysql\5.7.44`,
		DataDir:  `E:\DevStack\data\mysql\5.7.44`,
		Port:     3306,
		Version:  "5.7.44",
		MyIni:    `E:\DevStack\programs\mysql\5.7.44\my.ini`,
	}
	mysqlBin := filepath.Join(cfg.MySQLDir, "bin", "mysql.exe")
	mysqld := filepath.Join(cfg.MySQLDir, "bin", "mysqld.exe")

	// 1. 账号已存在：不应改动任何东西
	healed, err := mysqlutil.EnsureRootIPAccount(cfg)
	fmt.Printf("step1(expect false,nil): healed=%v err=%v\n", healed, err)
	if err != nil {
		os.Exit(1)
	}

	// 2. 删除 root@127.0.0.1 模拟"旧数据目录"
	drop := exec.Command(mysqlBin, "--host=127.0.0.1", "--port=3306", "-u", "root", "-e", "DROP USER IF EXISTS 'root'@'127.0.0.1';")
	_ = drop.Run()

	// 3. 再次引导：应自动重建账号
	healed, err = mysqlutil.EnsureRootIPAccount(cfg)
	fmt.Printf("step3(expect true,nil): healed=%v err=%v\n", healed, err)
	if err != nil {
		os.Exit(1)
	}

	// 4. 重新拉起正常实例（引导只负责停掉旧实例，恢复由调用方负责）
	start := exec.Command(mysqld, "--defaults-file="+cfg.MyIni, "--datadir="+cfg.DataDir, "--port=3306")
	start.Dir = cfg.MySQLDir
	if err := start.Start(); err != nil {
		fmt.Println("restart failed:", err)
		os.Exit(1)
	}
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if process.FindPIDByPort(3306) > 0 {
			break
		}
	}

	// 5. 验证连接
	ok := exec.Command(mysqlBin, "--host=127.0.0.1", "--port=3306", "-u", "root", "-e", "SELECT 1;")
	out, err := ok.CombinedOutput()
	fmt.Printf("step5 verify: err=%v out=%s\n", err, string(out))
}