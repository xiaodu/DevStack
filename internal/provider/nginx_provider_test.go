package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/resource"
)

// probe: php 基端口展开为 4 个工作进程端口，且包含 keepalive
func TestGeneratePHPUpstreamConfigExpandsWorkers(t *testing.T) {
	cfg := generatePHPUpstreamConfig([]int{8000})
	if !strings.Contains(cfg, "127.0.0.1:8000;") {
		t.Errorf("缺少 8000 端口: %s", cfg)
	}
	if !strings.Contains(cfg, "127.0.0.1:8003;") {
		t.Errorf("缺少 8003 端口（第 4 个工作进程）: %s", cfg)
	}
	if !strings.Contains(cfg, "keepalive 32") {
		t.Errorf("缺少 keepalive: %s", cfg)
	}
}

// probe: 多个 php 版本时上游应包含所有版本的工作进程端口
func TestGeneratePHPUpstreamConfigMultipleVersions(t *testing.T) {
	cfg := generatePHPUpstreamConfig([]int{8000, 8001})
	for _, port := range []int{8000, 8001, 8003, 8004} {
		if !strings.Contains(cfg, fmt.Sprintf("127.0.0.1:%d;", port)) {
			t.Errorf("上游缺少端口 %d: %s", port, cfg)
		}
	}
}

// probe: 无 php 端口时回退 9000 系列，保证配置始终合法
func TestGeneratePHPUpstreamConfigFallback(t *testing.T) {
	cfg := generatePHPUpstreamConfig(nil)
	if !strings.Contains(cfg, "127.0.0.1:9000;") {
		t.Errorf("缺少回退端口 9000: %s", cfg)
	}
	if strings.Contains(cfg, "127.0.0.1:9004;") {
		t.Errorf("回退端口不应超过 9000-9003: %s", cfg)
	}
}

// probe: applyConfig 必须把资源池中的 PHP 端口写入 php_backend 上游，
// 而不是用过期写死的 9000 系列（这是 localhost 出现 404 的根因）。
func TestApplyConfigWritesCurrentPHPPorts(t *testing.T) {
	installDir := t.TempDir()
	cfg := config.NewConfigForDir(installDir)
	resMgr := resource.NewManager(installDir)
	if _, err := resMgr.Ports().Allocate("php:7.4.33", 8000); err != nil {
		t.Fatalf("allocate php port: %v", err)
	}

	p := NewNginxProvider(cfg, nil, resMgr)
	inst, err := p.NewInstance(context.Background(), map[string]interface{}{"version": "1.26.0", "port": 80})
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	if err := p.applyConfig(inst.(*NginxInstance)); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(installDir, "programs", "nginx", "1.26.0", "conf", "nginx.conf"))
	if err != nil {
		t.Fatalf("read nginx.conf: %v", err)
	}
	conf := string(data)

	if !strings.Contains(conf, "upstream php_backend") {
		t.Errorf("nginx.conf 缺少 upstream php_backend")
	}
	for _, port := range []string{"8000;", "8001;", "8002;", "8003;"} {
		if !strings.Contains(conf, "127.0.0.1:"+port) {
			t.Errorf("nginx.conf 上游缺少 127.0.0.1:%s", port)
		}
	}
	if strings.Contains(conf, "127.0.0.1:9000;") {
		t.Errorf("nginx.conf 仍引用过期端口 9000")
	}
}
