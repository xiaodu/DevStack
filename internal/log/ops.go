package log

import (
	"fmt"
	"sync"
	"time"
)

type OpEntry struct {
	Time    string `json:"time"`
	Action  string `json:"action"`
	Service string `json:"service"`
	Detail  string `json:"detail"`
	Status  string `json:"status"`
}

type OpStore struct {
	mu      sync.RWMutex
	buf     []OpEntry
	max     int
	logger  *FileLogger
}

func NewOpStore(max int) *OpStore {
	return &OpStore{max: max}
}

func (s *OpStore) SetLogger(logger *FileLogger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger = logger
}

func (s *OpStore) Add(action, service, detail, status string) {
	s.mu.Lock()
	timestamp := time.Now().Format("15:04:05")
	s.buf = append(s.buf, OpEntry{
		Time:    timestamp,
		Action:  action,
		Service: service,
		Detail:  detail,
		Status:  status,
	})
	if len(s.buf) > s.max {
		s.buf = s.buf[len(s.buf)-s.max:]
	}
	logger := s.logger
	s.mu.Unlock()

	if logger != nil {
		logLevel := LevelInfo
		if status == "failed" || status == "error" || status == "失败" {
			logLevel = LevelError
		}
		entry := fmt.Sprintf("%s | %s | %s", action, detail, status)
		if service != "" {
			entry = fmt.Sprintf("[%s] %s", service, entry)
		}
		logger.log(logLevel, entry)
	}
}

func (s *OpStore) LogInfo(message string, args ...interface{}) {
	if s.logger != nil {
		s.logger.Info(message, args...)
	}
}

func (s *OpStore) LogWarn(message string, args ...interface{}) {
	if s.logger != nil {
		s.logger.Warn(message, args...)
	}
}

func (s *OpStore) LogError(message string, args ...interface{}) {
	if s.logger != nil {
		s.logger.Error(message, args...)
	}
}

func (s *OpStore) LogDebug(message string, args ...interface{}) {
	if s.logger != nil {
		s.logger.Debug(message, args...)
	}
}

func (s *OpStore) Recent(n int) []OpEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || n > len(s.buf) {
		n = len(s.buf)
	}
	return append([]OpEntry{}, s.buf[len(s.buf)-n:]...)
}

// Clear 清空操作记录缓冲并清空日志文件内容。
func (s *OpStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = s.buf[:0]
	if s.logger != nil {
		return s.logger.Clear()
	}
	return nil
}

func (s *OpStore) GetLogFiles() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.logger == nil {
		return nil, fmt.Errorf("文件日志未配置")
	}
	return s.logger.GetLogFiles()
}

func (s *OpStore) ReadLogFile(path string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.logger == nil {
		return nil, fmt.Errorf("文件日志未配置")
	}
	return s.logger.ReadLogFile(path)
}

func (s *OpStore) ReadRecentLines(count int) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.logger == nil {
		return nil, fmt.Errorf("文件日志未配置")
	}
	return s.logger.ReadRecentLines(count)
}
