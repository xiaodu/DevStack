package installdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCheckRejectsUnsafeTargets(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "DevStack")
	writeFile(t, filepath.Join(current, "config", "settings.toml"), "theme='light'")
	nonEmpty := filepath.Join(root, "busy")
	writeFile(t, filepath.Join(nonEmpty, "keep.txt"), "keep")

	cases := []struct {
		name   string
		target string
	}{
		{name: "same", target: current},
		{name: "inside", target: filepath.Join(current, "nested")},
		{name: "ancestor", target: root},
		{name: "relative", target: "newstack"},
		{name: "non-empty", target: nonEmpty},
		{name: "volume-root", target: filepath.VolumeName(root) + string(os.PathSeparator)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := Check(current, tc.target)
			if check.Valid {
				t.Fatalf("expected target %q to be rejected, got %+v", tc.target, check)
			}
			if len(check.Errors) == 0 {
				t.Fatalf("expected error message for %q", tc.target)
			}
		})
	}
}

func TestCheckAcceptsEmptyOrMissingTarget(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "DevStack")
	writeFile(t, filepath.Join(current, "config", "settings.toml"), "theme='light'")
	empty := filepath.Join(root, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{empty, filepath.Join(root, "missing", "NewStack")} {
		check := Check(current, target)
		if !check.Valid {
			t.Fatalf("target %q should be valid: %+v", target, check)
		}
		if check.SourceSize <= 0 || check.FileCount == 0 {
			t.Fatalf("source stats not collected: %+v", check)
		}
	}
}

func TestMigrateCopiesAndRewritesConfigPaths(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "DevStack")
	target := filepath.Join(root, "Moved", "DevStack")

	slashed := filepath.ToSlash(current)
	native := filepath.Clean(current)
	jsonEscaped := strings.ReplaceAll(native, `\`, `\\`)

	writeFile(t, filepath.Join(current, "config", "projects.json"),
		`{"root":"`+jsonEscaped+`/www/demo"}`)
	writeFile(t, filepath.Join(current, "programs", "nginx", "1.24.0", "conf", "nginx.conf"),
		"root "+slashed+"/www;")
	writeFile(t, filepath.Join(current, "programs", "mysql", "8.0.39", "conf", "my.ini"),
		"datadir="+native+`\data\mysql`+"\n")
	writeFile(t, filepath.Join(current, "data", "mysql", "payload.bin"),
		"keep "+native+"/data untouched")
	writeFile(t, filepath.Join(current, "www", "index.php"),
		"<?php // "+native+"/www should stay ?>"+strings.Repeat("x", 32))

	warnings, err := Migrate(current, target, nil)
	if err != nil {
		t.Fatalf("migrate: %v (warnings=%v)", err, warnings)
	}

	for _, rel := range []string{
		filepath.Join("config", "projects.json"),
		filepath.Join("programs", "nginx", "1.24.0", "conf", "nginx.conf"),
		filepath.Join("programs", "mysql", "8.0.39", "conf", "my.ini"),
		filepath.Join("data", "mysql", "payload.bin"),
		filepath.Join("www", "index.php"),
	} {
		if _, err := os.Stat(filepath.Join(target, rel)); err != nil {
			t.Fatalf("expected %s in target: %v", rel, err)
		}
	}

	assertContains := func(rel, want string) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(target, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if !strings.Contains(string(data), want) {
			t.Fatalf("%s should contain %q, got %q", rel, want, data)
		}
		if strings.Contains(strings.ToLower(string(data)), strings.ToLower(current)) {
			t.Fatalf("%s still contains old install dir: %q", rel, data)
		}
	}

	assertContains(filepath.Join("config", "projects.json"), strings.ReplaceAll(target, `\`, `\\`))
	assertContains(filepath.Join("programs", "nginx", "1.24.0", "conf", "nginx.conf"), filepath.ToSlash(target))
	assertContains(filepath.Join("programs", "mysql", "8.0.39", "conf", "my.ini"), target)

	data, err := os.ReadFile(filepath.Join(target, "data", "mysql", "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), native) {
		t.Fatalf("data file should not be rewritten, got %q", data)
	}

	if _, err := os.Stat(filepath.Join(root, "Moved", "DevStack")); err != nil {
		t.Fatalf("target missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "DevStack", "config", "projects.json")); err != nil {
		t.Fatalf("source should remain intact: %v", err)
	}
}

func TestMigrateReplacesExistingEmptyTarget(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "DevStack")
	writeFile(t, filepath.Join(current, "config", "settings.toml"), "theme='light'")
	target := filepath.Join(root, "NewStack")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Migrate(current, target, nil); err != nil {
		t.Fatalf("migrate into empty target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "config", "settings.toml")); err != nil {
		t.Fatalf("expected files in target: %v", err)
	}
}

func TestMigrateRefusesNonEmptyTargetWithoutTouchingIt(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "DevStack")
	writeFile(t, filepath.Join(current, "config", "settings.toml"), "theme='light'")
	target := filepath.Join(root, "Busy")
	writeFile(t, filepath.Join(target, "keep.txt"), "keep")

	if _, err := Migrate(current, target, nil); err == nil {
		t.Fatal("expected migration into non-empty target to fail")
	}
	data, err := os.ReadFile(filepath.Join(target, "keep.txt"))
	if err != nil {
		t.Fatalf("target content should stay intact: %v", err)
	}
	if string(data) != "keep" {
		t.Fatalf("target content changed: %q", data)
	}
}
