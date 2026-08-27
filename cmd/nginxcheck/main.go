package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/process"
	"github.com/xiaodu/DevStack/internal/provider"
	"github.com/xiaodu/DevStack/internal/resource"
)

// nginxcheck 验证"孤儿旧版本实例被自动接管"场景：
//
//	前置：:80 被任意旧版本 nginx（如 1.20.2 残留）占用
//	预期：启动 nginx@1.26.0 时自动清掉残留并成功上位，HTTP 返回 DevStack www 内容
func main() {
	cfg := config.NewConfigForDir(`E:\DevStack`)
	pm := process.NewManager()
	resMgr := resource.NewManager(filepath.Join(`E:\DevStack`, "config"))
	_ = provider.NewNginxProvider(cfg, pm, resMgr)

	p := provider.Get("nginx")
	if p == nil {
		fmt.Println("nginx provider 未注册")
		os.Exit(1)
	}

	if pid := process.FindPIDByPort(80); pid > 0 {
		fmt.Printf("前置确认: :80 当前被 PID %d (%s) 占用\n", pid, process.GetExecutablePath(pid))
	} else {
		fmt.Println("前置确认: :80 当前空闲（无孤儿可验证接管，仅验证正常启动）")
	}

	inst, err := p.NewInstance(context.Background(), map[string]interface{}{"version": "1.26.0", "port": 80})
	if err != nil {
		fmt.Println("NewInstance 失败:", err)
		os.Exit(1)
	}
	entity := inst.(interface {
		Start(ctx context.Context) error
		Stop(ctx context.Context) error
	})

	start := time.Now()
	if err := entity.Start(context.Background()); err != nil {
		fmt.Printf("启动 nginx@1.26.0 失败 (耗时 %s): %v\n", time.Since(start).Round(time.Millisecond), err)
		os.Exit(1)
	}
	fmt.Printf("启动 nginx@1.26.0 成功 (耗时 %s)\n", time.Since(start).Round(time.Millisecond))

	pid := process.FindPIDByPort(80)
	exe := process.GetExecutablePath(pid)
	fmt.Printf(":80 现由 PID %d (%s) 监听\n", pid, exe)
	if pid <= 0 || !contains(exe, `programs\nginx\1.26.0`) {
		fmt.Println("验证失败: :80 未被 1.26.0 接管")
		os.Exit(1)
	}

	resp, err := http.Get("http://127.0.0.1:80/")
	if err != nil {
		fmt.Println("HTTP 请求失败:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		fmt.Println("HTTP 响应为空")
		os.Exit(1)
	}
	fmt.Printf("HTTP %d, body 前 60 字节: %.60q\n", resp.StatusCode, string(body))

	_ = entity.Stop(context.Background())
	fmt.Println("验证通过")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOfFold(s, sub) >= 0
}

func indexOfFold(s, sub string) int {
	s, sub = toLower(s), toLower(sub)
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
