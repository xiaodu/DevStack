package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProcfile(t *testing.T) {
	content := `; DevStack Procfile
; 格式：; Name: [autorun] COMMAND [ARGS...] [KEY=VALUE...] [pwd=WORKDIR] [grace=N]

; Nginx: nginx.exe -p C:\nginx
; MySQL: mysqld.exe --defaults-file=my.ini
; PHP: php-cgi.exe -b 127.0.0.1:9000
; Redis: redis-server.exe
; Node: autorun node start PORT=3000 PWD=C:\www\myapp
; Worker: python worker.py grace=10
`
	path := createTempProcfile(t, content)
	defer os.Remove(path)

	parser, err := NewProcfileParser(path)
	if err != nil {
		t.Fatalf("NewProcfileParser failed: %v", err)
	}

	procfile, err := parser.Parse(path)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(procfile.Entries) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(procfile.Entries))
	}

	// 测试 Nginx
	nginx := procfile.Entries[0]
	if nginx.Name != "Nginx" {
		t.Errorf("expected name 'Nginx', got '%s'", nginx.Name)
	}
	if nginx.Command != "nginx.exe" {
		t.Errorf("expected command 'nginx.exe', got '%s'", nginx.Command)
	}
	if len(nginx.Args) != 2 || nginx.Args[0] != "-p" || nginx.Args[1] != "C:\\nginx" {
		t.Errorf("expected args [-p C:\\nginx], got %v", nginx.Args)
	}

	// 测试 MySQL
	mysql := procfile.Entries[1]
	if mysql.Name != "MySQL" {
		t.Errorf("expected name 'MySQL', got '%s'", mysql.Name)
	}
	if mysql.Command != "mysqld.exe" {
		t.Errorf("expected command 'mysqld.exe', got '%s'", mysql.Command)
	}

	// 测试 PHP
	php := procfile.Entries[2]
	if php.Name != "PHP" {
		t.Errorf("expected name 'PHP', got '%s'", php.Name)
	}
	if php.Command != "php-cgi.exe" {
		t.Errorf("expected command 'php-cgi.exe', got '%s'", php.Command)
	}
	if len(php.Args) != 2 || php.Args[0] != "-b" || php.Args[1] != "127.0.0.1:9000" {
		t.Errorf("expected args [-b 127.0.0.1:9000], got %v", php.Args)
	}

	// 测试 Redis
	redis := procfile.Entries[3]
	if redis.Name != "Redis" {
		t.Errorf("expected name 'Redis', got '%s'", redis.Name)
	}
	if redis.Command != "redis-server.exe" {
		t.Errorf("expected command 'redis-server.exe', got '%s'", redis.Command)
	}

	// 测试 Node (autorun)
	node := procfile.Entries[4]
	if node.Name != "Node" {
		t.Errorf("expected name 'Node', got '%s'", node.Name)
	}
	if !node.AutoRun {
		t.Error("expected autorun to be true")
	}
	if node.Command != "node" {
		t.Errorf("expected command 'node', got '%s'", node.Command)
	}
	if node.Env["PORT"] != "3000" {
		t.Errorf("expected PORT=3000, got %v", node.Env)
	}
	if node.WorkDir != "C:\\www\\myapp" {
		t.Errorf("expected WorkDir 'C:\\www\\myapp', got '%s'", node.WorkDir)
	}

	// 测试 Worker (grace)
	worker := procfile.Entries[5]
	if worker.Name != "Worker" {
		t.Errorf("expected name 'Worker', got '%s'", worker.Name)
	}
	if worker.GraceSec != 10 {
		t.Errorf("expected GraceSec 10, got %d", worker.GraceSec)
	}
}

func TestParseProcfileWithQuotes(t *testing.T) {
	content := `; Service: "C:\Program Files\app\app.exe" --name="my service"
`
	path := createTempProcfile(t, content)
	defer os.Remove(path)

	parser, err := NewProcfileParser(path)
	if err != nil {
		t.Fatalf("NewProcfileParser failed: %v", err)
	}

	procfile, err := parser.Parse(path)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(procfile.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(procfile.Entries))
	}

	svc := procfile.Entries[0]
	if svc.Command != "C:\\Program Files\\app\\app.exe" {
		t.Errorf("expected command 'C:\\Program Files\\app\\app.exe', got '%s'", svc.Command)
	}
	if len(svc.Args) != 1 || svc.Args[0] != "--name=my service" {
		t.Errorf("expected args [--name=my service], got %v", svc.Args)
	}
}

func TestParseProcfileEmptyLines(t *testing.T) {
	content := `
; Comment line

; Service1: cmd1.exe

; Service2: cmd2.exe

`
	path := createTempProcfile(t, content)
	defer os.Remove(path)

	parser, err := NewProcfileParser(path)
	if err != nil {
		t.Fatalf("NewProcfileParser failed: %v", err)
	}

	procfile, err := parser.Parse(path)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(procfile.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(procfile.Entries))
	}
}

func TestParseProcfileDuplicateName(t *testing.T) {
	content := `; Nginx: nginx.exe
; Nginx: nginx2.exe
`
	path := createTempProcfile(t, content)
	defer os.Remove(path)

	parser, err := NewProcfileParser(path)
	if err != nil {
		t.Fatalf("NewProcfileParser failed: %v", err)
	}

	_, err = parser.Parse(path)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestParseProcfileMissingColon(t *testing.T) {
	content := `; Nginx nginx.exe
; Redis: redis-server.exe
`
	path := createTempProcfile(t, content)
	defer os.Remove(path)

	parser, err := NewProcfileParser(path)
	if err != nil {
		t.Fatalf("NewProcfileParser failed: %v", err)
	}

	procfile, err := parser.Parse(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(procfile.Entries) != 1 {
		t.Fatalf("expected 1 entry (Nginx line skipped as comment), got %d", len(procfile.Entries))
	}
	if procfile.Entries[0].Name != "Redis" {
		t.Errorf("expected entry name 'Redis', got '%s'", procfile.Entries[0].Name)
	}
}

func TestParseProcfileMissingCommand(t *testing.T) {
	content := `; Nginx:
`
	path := createTempProcfile(t, content)
	defer os.Remove(path)

	parser, err := NewProcfileParser(path)
	if err != nil {
		t.Fatalf("NewProcfileParser failed: %v", err)
	}

	_, err = parser.Parse(path)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestSplitParts(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"cmd arg1 arg2", []string{"cmd", "arg1", "arg2"}},
		{"cmd \"arg with space\"", []string{"cmd", "arg with space"}},
		{"cmd 'arg with space'", []string{"cmd", "arg with space"}},
		{"cmd arg1 \"arg 2\" arg3", []string{"cmd", "arg1", "arg 2", "arg3"}},
		{"cmd", []string{"cmd"}},
		{"", nil},
		{"  cmd  arg1  ", []string{"cmd", "arg1"}},
	}

	for _, tt := range tests {
		result := splitParts(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("splitParts(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("splitParts(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}

func TestParseKeyValue(t *testing.T) {
	tests := []struct {
		input   string
		key     string
		value   string
		isValid bool
	}{
		{"KEY=value", "KEY", "value", true},
		{"PORT=3000", "PORT", "3000", true},
		{"MY_VAR=test", "MY_VAR", "test", true},
		{"not-a-key", "", "", false},
		{"=value", "", "", false},
		{"KEY=", "KEY", "", true},
		{"KEY=value with spaces", "KEY", "value with spaces", true},
	}

	for _, tt := range tests {
		result, ok := parseKeyValue(tt.input)
		if ok != tt.isValid {
			t.Errorf("parseKeyValue(%q) ok = %v, want %v", tt.input, ok, tt.isValid)
			continue
		}
		if ok && result[0] != tt.key {
			t.Errorf("parseKeyValue(%q) key = %q, want %q", tt.input, result[0], tt.key)
		}
		if ok && result[1] != tt.value {
			t.Errorf("parseKeyValue(%q) value = %q, want %q", tt.input, result[1], tt.value)
		}
	}
}

func TestExtractPorts(t *testing.T) {
	tests := []struct {
		command string
		args    []string
		ports   []int
	}{
		{"php-cgi.exe", []string{"-b", "127.0.0.1:9000"}, []int{9000}},
		{"node", []string{"start", "--port=3000"}, []int{3000}},
		{"nginx.exe", []string{"-p", "C:\\nginx"}, []int{}},
		{"app.exe", []string{":8080"}, []int{8080}},
		{"app.exe", []string{"0.0.0.0:5000"}, []int{5000}},
	}

	for _, tt := range tests {
		result := extractPorts(tt.command, tt.args)
		if len(result) != len(tt.ports) {
			t.Errorf("extractPorts(%q, %v) = %v, want %v", tt.command, tt.args, result, tt.ports)
			continue
		}
		for i := range result {
			if result[i] != tt.ports[i] {
				t.Errorf("extractPorts(%q, %v)[%d] = %d, want %d", tt.command, tt.args, i, result[i], tt.ports[i])
			}
		}
	}
}

func TestWriteProcfile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "Procfile")

	entries := []ProcfileEntry{
		{Name: "Nginx", Command: "nginx.exe", Args: []string{"-p", "C:\\nginx"}},
		{Name: "MySQL", Command: "mysqld.exe", GraceSec: 10},
	}

	err := WriteProcfile(path, entries)
	if err != nil {
		t.Fatalf("WriteProcfile failed: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Procfile not created")
	}

	// 验证可以解析
	parser, err := NewProcfileParser(path)
	if err != nil {
		t.Fatalf("NewProcfileParser failed: %v", err)
	}

	procfile, err := parser.Parse(path)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(procfile.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(procfile.Entries))
	}
}

// createTempProcfile 创建临时 Procfile 文件
func createTempProcfile(t *testing.T, content string) string {
	t.Helper()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "Procfile")

	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create temp Procfile: %v", err)
	}

	return path
}
