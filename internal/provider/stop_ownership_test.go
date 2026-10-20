package provider

import (
	"path/filepath"
	"testing"
)

func win(p string) string { return filepath.FromSlash(p) }

// 归属判定必须是"目录前缀 + 分隔符边界"，且必须收到版本粒度：
// 旧实现用 strings.Contains 且按 kind 目录判定，结果是启动 1.26 会静默强杀正在运行的 1.24。
func TestPathWithinDir(t *testing.T) {
	versionDir := win(`E:\DevStack\programs\nginx\1.26.2`)
	sameDirExe := win(`E:\DevStack\programs\nginx\1.26.2\nginx.exe`)
	siblingExe := win(`E:\DevStack\programs\nginx\1.24.0\nginx.exe`)
	lookalike := win(`D:\tools\copy-of-nginx\1.26.2\nginx.exe`)
	parentExe := win(`E:\DevStack\programs\nginx\nginx.exe`)

	cases := []struct {
		exe  string
		dir  string
		want bool
		desc string
	}{
		{sameDirExe, versionDir, true, "本版本目录内"},
		{siblingExe, versionDir, false, "兄弟版本目录不算自家"},
		{lookalike, versionDir, false, "路径里含同名片段不算自家（旧 Contains 会误判）"},
		{parentExe, versionDir, false, "父目录里的 exe 不算自家"},
		{sameDirExe, win(`E:\DEVSTACK\PROGRAMS\NGINX\1.26.2`), true, "大小写不敏感"},
		{"", versionDir, false, "空路径"},
		{sameDirExe, "", false, "空目录"},
	}
	for _, c := range cases {
		if got := pathWithinDir(c.exe, c.dir); got != c.want {
			t.Errorf("%s: pathWithinDir(%q,%q)=%v, 期望 %v", c.desc, c.exe, c.dir, got, c.want)
		}
	}
}

func TestSiblingVersionOf(t *testing.T) {
	versionDir := win(`E:\DevStack\programs\nginx\1.26.2`)

	if got := siblingVersionOf(win(`E:\DevStack\programs\nginx\1.24.0\nginx.exe`), versionDir); got != "1.24.0" {
		t.Errorf("应识别出兄弟版本 1.24.0, 实际 %q", got)
	}
	if got := siblingVersionOf(win(`E:\DevStack\programs\nginx\1.26.2\nginx.exe`), versionDir); got != "" {
		t.Errorf("本版本自身不该算兄弟, 实际 %q", got)
	}
	if got := siblingVersionOf(win(`C:\nginx\nginx.exe`), versionDir); got != "" {
		t.Errorf("外部程序不应被说成兄弟版本, 实际 %q", got)
	}
}
