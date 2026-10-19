package service

import (
	"path/filepath"
	"testing"

	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/resource"
)

// 回归：Configure 曾按裸 kind 选中版本后，又用原始入参回查服务表，
// 导致前端 applyServiceConfig('nginx') 恒失败（"配置事务失败"）。
// 这里固化"选择规则"本身：解析结果必须是服务表里真实存在的完整 ID。

func nginxMgr(instances ...*ServiceInstance) *Manager {
	m := &Manager{services: map[string]*ServiceInstance{}}
	for _, s := range instances {
		m.services[s.Info.ID] = s
	}
	return m
}

func inst(id, kind, version string, status core.ServiceStatus) *ServiceInstance {
	return &ServiceInstance{Info: core.ServiceInfo{
		ID: id, Kind: kind, Version: version, Status: status,
	}}
}

func TestResolveConfigTarget_ExactID(t *testing.T) {
	m := nginxMgr(
		inst("nginx@1.24.0", "nginx", "1.24.0", core.StatusStopped),
		inst("nginx@1.26.2", "nginx", "1.26.2", core.StatusRunning),
	)
	id, s := m.resolveConfigTarget("nginx@1.24.0")
	if s == nil || id != "nginx@1.24.0" {
		t.Fatalf("完整 ID 应原样命中, 实际 id=%q", id)
	}
}

func TestResolveConfigTarget_RunningWins(t *testing.T) {
	m := nginxMgr(
		inst("nginx@1.26.2", "nginx", "1.26.2", core.StatusRunning),
		inst("nginx@1.24.0", "nginx", "1.24.0", core.StatusStopped),
	)
	id, _ := m.resolveConfigTarget("nginx")
	if id != "nginx@1.26.2" {
		t.Fatalf("裸 kind 应优先解析到运行中的实例, 实际 %q", id)
	}
}

func TestResolveConfigTarget_DefaultThenHighest(t *testing.T) {
	// 注册表里 nginx 的默认版本
	def := defaultVersionOf("nginx")
	if def == "" {
		t.Skip("注册表未配置 nginx 默认版本")
	}
	other := "1.2.3"
	if versionGreater(def, other) {
		other = "9.9.9"
	}
	m := nginxMgr(
		inst("nginx@"+other, "nginx", other, core.StatusStopped),
		inst("nginx@"+def, "nginx", def, core.StatusStopped),
	)
	id, _ := m.resolveConfigTarget("nginx")
	if id != "nginx@"+def {
		t.Fatalf("无运行实例时应选注册表默认版本 %q, 实际 %q", "nginx@"+def, id)
	}
}

func TestResolveConfigTarget_UnknownKind(t *testing.T) {
	m := nginxMgr(inst("nginx@1.24.0", "nginx", "1.24.0", core.StatusStopped))
	if id, s := m.resolveConfigTarget("nope"); s != nil || id != "" {
		t.Fatalf("未知 kind 应解析失败, 实际 id=%q", id)
	}
}

// 回归：版本高低曾用字符串比较，"3.9.13" 会被判为高于 "3.12.8"。
func TestVersionGreater_NumericSegments(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"3.12.8", "3.9.13", true},
		{"3.9.13", "3.12.8", false},
		{"1.26.2", "1.26.2", false},
		{"8.0.36", "8.4.3", false},
		{"8.4.3", "8.0.36", true},
		{"1.2", "1.2.0", false},
		{"1.2.1", "1.2", true},
		{"22.11.0", "3.13.2", true},
	}
	for _, c := range cases {
		if got := versionGreater(c.a, c.b); got != c.want {
			t.Errorf("versionGreater(%q,%q)=%v, 期望 %v", c.a, c.b, got, c.want)
		}
	}
}

func TestResolveConfigTarget_HighestIsNumericNotLexical(t *testing.T) {
	// 两个都未运行、都不是默认版本时取最高版本；字符串序会选成 3.9.13。
	m := nginxMgr(
		inst("python@3.9.13", "python", "3.9.13", core.StatusStopped),
		inst("python@3.12.8", "python", "3.12.8", core.StatusStopped),
	)
	def := defaultVersionOf("python")
	if def == "3.9.13" || def == "3.12.8" {
		t.Skip("默认版本正好命中其中之一，无法验证最高版本分支")
	}
	id, _ := m.resolveConfigTarget("python")
	if id != "python@3.12.8" {
		t.Fatalf("应取数字意义上的最高版本 3.12.8, 实际 %q", id)
	}
}

// 回归：首跑向导之前安装目录为空、resMgr 为 nil，向导结束后必须能把依赖补进去，
// 否则端口分配与配置落盘会走空指针。
func TestSetResourceManagerInjectsAfterWizard(t *testing.T) {
	m := &Manager{services: map[string]*ServiceInstance{}}
	if m.resourceMgr() != nil {
		t.Fatalf("初始应没有资源管理器")
	}
	rm := resource.NewManager(filepath.Join(t.TempDir(), "config"))
	m.SetResourceManager(rm)
	if m.resourceMgr() != rm {
		t.Fatalf("SetResourceManager 后应能读到注入的管理器")
	}
}
