package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaodu/DevStack/internal/core"
)

// 回归：站点配置曾写死在 nginx 1.26.0 目录，实际运行 1.24.0 时域名落回默认站点。
// 现在必须同步到所有已安装版本。
func TestSyncNginxWritesToAllInstalledVersions(t *testing.T) {
	dir := t.TempDir()
	versions := []string{"1.24.0", "1.26.0"}
	for _, v := range versions {
		base := filepath.Join(dir, "programs", "nginx", v)
		if err := os.MkdirAll(filepath.Join(base, "conf"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "nginx.exe"), []byte("fake"), 0o755); err != nil {
			t.Fatal(err)
		}
		conf := "http {\n    include sites-enabled/*.conf;\n}\n"
		if err := os.WriteFile(filepath.Join(base, "conf", "nginx.conf"), []byte(conf), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := NewManager(dir, nil, nil)
	m.projects["p1"] = &core.Project{
		ID:      "p1",
		Name:    "clouderp",
		Type:    core.ProjectStatic,
		Root:    filepath.Join(dir, "app"),
		Domain:  "clouderp.local",
		Port:    80,
		Enabled: true,
	}
	m.SyncNginx()

	for _, v := range versions {
		path := filepath.Join(dir, "programs", "nginx", v, "conf", "sites-enabled", "p1.conf")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("nginx %s 未生成站点配置: %v", v, err)
		}
		if !strings.Contains(string(data), "server_name clouderp.local;") {
			t.Fatalf("nginx %s 站点配置缺少域名: %s", v, data)
		}
	}
}
