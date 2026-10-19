package main

import (
	"path/filepath"
	"testing"

	"github.com/xiaodu/DevStack/internal/config"
)

// 回归：nginx/mysql 的日志分支曾用裸 kind 去查以 kind@version 为键的服务表，
// 恒查不到 → 这两个组件的日志一直是空的。现在所有组件走同一条 kind@version 拆分逻辑。
func TestResolveServiceLogPath(t *testing.T) {
	dir := t.TempDir()
	a := &App{cfg: config.NewConfigForDir(dir)}

	cases := []struct {
		id       string
		wantRel  string
		wantNone bool
	}{
		{id: "nginx@1.26.2", wantRel: filepath.Join("programs", "nginx", "1.26.2", "logs", "error.log")},
		{id: "mysql@8.4.3", wantRel: filepath.Join("programs", "mysql", "8.4.3", "error.log")},
		{id: "redis@7.4.2", wantRel: filepath.Join("programs", "redis", "7.4.2", "redis.log")},
		{id: "php@8.4.23", wantRel: filepath.Join("programs", "php", "8.4.23", "php_errors.log")},
		{id: "node@22.11.0", wantRel: filepath.Join("programs", "node", "22.11.0", "node.log")},
		{id: "python@3.13.2", wantRel: filepath.Join("programs", "python", "3.13.2", "python.log")},
		{id: "apache@2.4.62", wantNone: true},      // 未登记日志位置 → 不猜测
		{id: "not-a-service", wantNone: true},      // 未知 kind
		{id: "nginx@../../config", wantNone: true}, // 版本段越权
		{id: "nginx@..", wantNone: true},
	}
	for _, c := range cases {
		got := a.resolveServiceLogPath(c.id)
		if c.wantNone {
			if got != "" {
				t.Errorf("ResolveServiceLogPath(%q) = %q, 期望空", c.id, got)
			}
			continue
		}
		want := filepath.Join(dir, c.wantRel)
		if got != want {
			t.Errorf("ResolveServiceLogPath(%q)\n  = %q\n  期望 %q", c.id, got, want)
		}
	}
}

func TestSplitServiceID(t *testing.T) {
	cases := []struct {
		id, kind, version string
	}{
		{"nginx@1.26.2", "nginx", "1.26.2"},
		{"nginx", "nginx", ""},
		{"php@8.4.23", "php", "8.4.23"},
	}
	for _, c := range cases {
		k, v := splitServiceID(c.id)
		if k != c.kind || v != c.version {
			t.Errorf("splitServiceID(%q) = (%q,%q), 期望 (%q,%q)", c.id, k, v, c.kind, c.version)
		}
	}
}
