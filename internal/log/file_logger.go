package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

type LogLevel string

const (
	LevelInfo  LogLevel = "INFO"
	LevelWarn  LogLevel = "WARN"
	LevelError LogLevel = "ERROR"
	LevelDebug LogLevel = "DEBUG"
)

type FileLogger struct {
	logger *slog.Logger
	writer *lumberjack.Logger
	logDir string
	level  LogLevel
}

type FileLoggerConfig struct {
	LogDir  string
	MaxSize int64
	MaxDays int
	Level   LogLevel
}

var slogLevelMap = map[LogLevel]slog.Level{
	LevelDebug: slog.LevelDebug,
	LevelInfo:  slog.LevelInfo,
	LevelWarn:  slog.LevelWarn,
	LevelError: slog.LevelError,
}

func NewFileLogger(config FileLoggerConfig) (*FileLogger, error) {
	if config.LogDir == "" {
		return nil, fmt.Errorf("日志目录不能为空")
	}
	if config.MaxSize <= 0 {
		config.MaxSize = 10 * 1024 * 1024
	}
	if config.MaxDays <= 0 {
		config.MaxDays = 30
	}
	if config.Level == "" {
		config.Level = LevelInfo
	}

	if err := os.MkdirAll(config.LogDir, 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	writer := &lumberjack.Logger{
		Filename:   filepath.Join(config.LogDir, "devstack.log"),
		MaxSize:    int(config.MaxSize / 1024 / 1024),
		MaxBackups: config.MaxDays,
		MaxAge:     config.MaxDays,
		LocalTime:  true,
	}

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slogLevelMap[config.Level],
	})

	return &FileLogger{
		logger: slog.New(handler),
		writer: writer,
		logDir: config.LogDir,
		level:  config.Level,
	}, nil
}

func (l *FileLogger) shouldLog(level LogLevel) bool {
	levels := []LogLevel{LevelDebug, LevelInfo, LevelWarn, LevelError}
	currentIdx := -1
	levelIdx := -1
	for i, lvl := range levels {
		if lvl == l.level {
			currentIdx = i
		}
		if lvl == level {
			levelIdx = i
		}
	}
	return levelIdx >= currentIdx
}

func (l *FileLogger) log(level LogLevel, message string, args ...interface{}) {
	if !l.shouldLog(level) {
		return
	}
	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}
	l.logger.LogAttrs(context.Background(), slogLevelMap[level], message)
}

func (l *FileLogger) Info(message string, args ...interface{}) {
	l.log(LevelInfo, message, args...)
}

func (l *FileLogger) Warn(message string, args ...interface{}) {
	l.log(LevelWarn, message, args...)
}

func (l *FileLogger) Error(message string, args ...interface{}) {
	l.log(LevelError, message, args...)
}

func (l *FileLogger) Debug(message string, args ...interface{}) {
	l.log(LevelDebug, message, args...)
}

func (l *FileLogger) GetLogFiles() ([]string, error) {
	files, err := os.ReadDir(l.logDir)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		if strings.HasPrefix(name, "devstack") && strings.HasSuffix(name, ".log") {
			result = append(result, filepath.Join(l.logDir, name))
		}
	}
	return result, nil
}

func (l *FileLogger) ReadLogFile(path string) ([]string, error) {
	// 防止读取日志目录之外的文件
	clean := filepath.Clean(path)
	dir := filepath.Clean(l.logDir)
	if clean != dir && !strings.HasPrefix(clean, dir+string(os.PathSeparator)) {
		return nil, fmt.Errorf("日志文件路径越界: %s", path)
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return nil, err
	}
	return splitLines(string(data)), nil
}

func (l *FileLogger) ReadRecentLines(count int) ([]string, error) {
	data, err := os.ReadFile(l.writer.Filename)
	if err != nil {
		return nil, err
	}
	lines := splitLines(string(data))
	if len(lines) > count {
		return lines[len(lines)-count:], nil
	}
	return lines, nil
}

func (l *FileLogger) Writer() io.Writer {
	return l.writer
}

func (l *FileLogger) Close() {
	l.writer.Close()
}

// Clear 清空当前日志文件内容（后续写入会自动重新打开文件）。
func (l *FileLogger) Clear() error {
	if l.writer == nil {
		return nil
	}
	l.writer.Close()
	if _, err := os.Stat(l.writer.Filename); os.IsNotExist(err) {
		return nil
	}
	return os.Truncate(l.writer.Filename, 0)
}

func splitLines(s string) []string {
	var lines []string
	var current []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if len(current) > 0 {
				lines = append(lines, string(current))
			}
			current = current[:0]
		} else {
			current = append(current, s[i])
		}
	}
	if len(current) > 0 {
		lines = append(lines, string(current))
	}
	return lines
}