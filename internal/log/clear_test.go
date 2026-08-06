package log

import (
	"path/filepath"
	"testing"
)

func TestFileLoggerClearTruncates(t *testing.T) {
	dir := t.TempDir()
	l, err := NewFileLogger(FileLoggerConfig{
		LogDir:  dir,
		MaxSize: 1024 * 1024,
		MaxDays: 30,
		Level:   LevelDebug,
	})
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	defer l.Close()

	l.Info("before-clear")
	if lines, err := l.ReadRecentLines(10); err != nil || len(lines) == 0 {
		t.Fatalf("expected logs before clear, got %d lines, err=%v", len(lines), err)
	}

	if err := l.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	lines, err := l.ReadRecentLines(10)
	if err != nil {
		t.Fatalf("ReadRecentLines after clear: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("expected empty logs after clear, got %d lines: %v", len(lines), lines)
	}

	l.Info("after-clear")
	lines, err = l.ReadRecentLines(10)
	if err != nil || len(lines) == 0 {
		t.Fatalf("expected logs to be writable after clear, got %d lines, err=%v", len(lines), err)
	}
	if filepath.Base(l.writer.Filename) != "devstack.log" {
		t.Fatalf("unexpected log file name: %s", l.writer.Filename)
	}
}

func TestOpStoreClearEmptiesBuffer(t *testing.T) {
	s := NewOpStore(100)
	s.Add("下载", "nginx", "1.24.0", "成功")
	s.Add("启动", "nginx", "", "成功")
	if n := len(s.Recent(10)); n != 2 {
		t.Fatalf("expected 2 entries before clear, got %d", n)
	}

	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if n := len(s.Recent(10)); n != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", n)
	}

	s.Add("停止", "nginx", "", "成功")
	if n := len(s.Recent(10)); n != 1 {
		t.Fatalf("expected 1 entry after re-add, got %d", n)
	}
}
