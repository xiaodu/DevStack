package environment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	return NewManager(dir), dir
}

// 契约: 内置预设可编辑（与普通环境一致），且修改必须持久化，重启后不丢失
func TestUpdateBuiltinPersists(t *testing.T) {
	m, dir := newTestManager(t)
	builtin, err := m.Get("builtin-lnmp-php74")
	if err != nil {
		t.Fatalf("Get builtin: %v", err)
	}
	builtin.Name = "自定义的 LNMP"
	if _, err := m.Update(builtin); err != nil {
		t.Fatalf("内置预设更新应被允许: %v", err)
	}
	// 内存中已生效
	after, _ := m.Get("builtin-lnmp-php74")
	if after.Name != "自定义的 LNMP" {
		t.Errorf("更新未生效, got %q", after.Name)
	}
	// 重新加载后修改仍在（不允许丢失）
	m2 := NewManager(dir)
	reloaded, _ := m2.Get("builtin-lnmp-php74")
	if reloaded.Name != "自定义的 LNMP" {
		t.Errorf("内置预设修改未持久化, reloaded got %q", reloaded.Name)
	}
}

// 契约: 内置预设可删除（与普通环境一致），且删除必须持久化，重启后不复活
func TestDeleteBuiltinPersists(t *testing.T) {
	m, dir := newTestManager(t)
	if err := m.Delete("builtin-lnmp-php74"); err != nil {
		t.Fatalf("内置预设删除应被允许: %v", err)
	}
	if _, err := m.Get("builtin-lnmp-php74"); err == nil {
		t.Errorf("删除后仍能获取到内置预设")
	}
	// 重新加载后不应复活
	m2 := NewManager(dir)
	if _, err := m2.Get("builtin-lnmp-php74"); err == nil {
		t.Errorf("内置预设删除未持久化，重启后复活")
	}
}

// probe: v4 文档「不存在裸 kind 操作」— 空 kind 或空 version 的 Unit 必须被拒绝
func TestCreateRejectsEmptyUnit(t *testing.T) {
	m, _ := newTestManager(t)
	env := Environment{
		Name: "非法环境",
		Units: []UnitSpec{
			{Kind: "nginx", Version: "1.24.0"},
			{Kind: "", Version: "7.2.5"}, // 裸版本
		},
	}
	if _, err := m.Create(env); err == nil {
		t.Errorf("包含空 kind 的 Unit 被允许创建，违反「不存在裸 kind 操作」约束")
	}

	env2 := Environment{
		Name: "非法环境2",
		Units: []UnitSpec{
			{Kind: "nginx", Version: "1.24.0"},
			{Kind: "mysql", Version: ""}, // 无版本
		},
	}
	if _, err := m.Create(env2); err == nil {
		t.Errorf("包含空 version 的 Unit 被允许创建，违反「不存在无版本的 Unit」约束")
	}
}

// 回归：环境编辑器里能选 Memcached，校验却只认旧的六种，导致保存直接失败。
func TestCreateAcceptsMemcachedUnit(t *testing.T) {
	m, _ := newTestManager(t)
	env, err := m.Create(Environment{
		Name: "带缓存的 LNMP",
		Units: []UnitSpec{
			{Kind: "nginx", Version: "1.24.0"},
			{Kind: "mysql", Version: "5.7.44"},
			{Kind: "php", Version: "7.4.33"},
			{Kind: "memcached", Version: "1.6.27"},
		},
	})
	if err != nil {
		t.Fatalf("memcached 组件应被接受: %v", err)
	}
	if len(env.Units) != 4 {
		t.Fatalf("组件数量不符: %+v", env.Units)
	}
}

// probe: 重复 ID 创建必须报错
func TestCreateDuplicateIDRejected(t *testing.T) {
	m, _ := newTestManager(t)
	env := Environment{
		ID:   "dup-env",
		Name: "重复环境",
		Units: []UnitSpec{
			{Kind: "nginx", Version: "1.24.0"},
		},
	}
	if _, err := m.Create(env); err != nil {
		t.Fatalf("首次 Create: %v", err)
	}
	if _, err := m.Create(env); err == nil {
		t.Errorf("重复 ID 创建未被拒绝")
	}
}

// probe: CRUD 持久化往返 — 自定义环境必须写入 environments.json 且可被重新加载
func TestCustomEnvironmentPersistRoundTrip(t *testing.T) {
	m, dir := newTestManager(t)
	created, err := m.Create(Environment{
		Name: "我的环境",
		Units: []UnitSpec{
			{Kind: "nginx", Version: "1.24.0"},
			{Kind: "redis", Version: "7.2.5"},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("Create 未生成 ID")
	}

	// 文件确实写了
	if _, err := os.Stat(filepath.Join(dir, "config", "environments.json")); err != nil {
		t.Errorf("environments.json 未写入: %v", err)
	}

	// 新建一个 Manager 重新加载
	m2 := NewManager(dir)
	loaded, err := m2.Get(created.ID)
	if err != nil {
		t.Fatalf("重新加载后 Get: %v", err)
	}
	if loaded.Name != "我的环境" || len(loaded.Units) != 2 {
		t.Errorf("重新加载后数据不一致: %+v", loaded)
	}

	// Update 后再持久化
	loaded.Name = "改名环境"
	if _, err := m2.Update(loaded); err != nil {
		t.Fatalf("Update: %v", err)
	}
	m3 := NewManager(dir)
	reloaded, _ := m3.Get(created.ID)
	if reloaded.Name != "改名环境" {
		t.Errorf("Update 未持久化, got %q", reloaded.Name)
	}

	// Delete 后持久化
	if err := m3.Delete(created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	m4 := NewManager(dir)
	if _, err := m4.Get(created.ID); err == nil {
		t.Errorf("Delete 未持久化，环境仍存在")
	}
}

// probe: 内置预设必须始终存在（重新加载后不会丢失）
func TestBuiltinsAlwaysPresent(t *testing.T) {
	m, dir := newTestManager(t)
	list := m.List()
	builtins := 0
	for _, e := range list {
		if e.IsBuiltin {
			builtins++
		}
	}
	if builtins == 0 {
		t.Fatalf("初始化后没有任何内置预设")
	}

	m2 := NewManager(dir)
	list2 := m2.List()
	builtins2 := 0
	for _, e := range list2 {
		if e.IsBuiltin {
			builtins2++
		}
	}
	if builtins2 != builtins {
		t.Errorf("内置预设数不一致, before=%d after=%d", builtins, builtins2)
	}
}

// probe: 空 kind/version 字符串（纯空白）也必须被拒绝
func TestCreateRejectsWhitespaceUnit(t *testing.T) {
	m, _ := newTestManager(t)
	env := Environment{
		Name: "空白环境",
		Units: []UnitSpec{
			{Kind: "   ", Version: "  "},
		},
	}
	if _, err := m.Create(env); err == nil {
		t.Errorf("纯空白 kind/version 被允许创建")
	}
}

// probe: Get 不存在的环境必须返回错误
func TestGetMissingReturnsError(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.Get("does-not-exist"); err == nil {
		t.Errorf("Get 不存在的环境未返回错误")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("错误信息缺少上下文: %v", err)
	}
}
