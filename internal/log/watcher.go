package log

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type LogEntry struct {
	Time    string `json:"time"`
	Content string `json:"content"`
	Stream  string `json:"stream"`
}

type LogWatcher struct {
	mu        sync.RWMutex
	listeners map[string]chan LogEntry
}

func NewLogWatcher() *LogWatcher {
	return &LogWatcher{
		listeners: make(map[string]chan LogEntry),
	}
}

func (w *LogWatcher) Subscribe(id string) chan LogEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch := make(chan LogEntry, 100)
	w.listeners[id] = ch
	return ch
}

func (w *LogWatcher) Unsubscribe(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ch, ok := w.listeners[id]; ok {
		close(ch)
		delete(w.listeners, id)
	}
}

func (w *LogWatcher) emit(entry LogEntry) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, ch := range w.listeners {
		select {
		case ch <- entry:
		default:
		}
	}
}

func (w *LogWatcher) TailFile(logPath string, stream string) {
	absPath, err := filepath.Abs(logPath)
	if err != nil {
		return
	}
	for {
		file, err := os.Open(absPath)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		file.Seek(0, io.SeekEnd)
		reader := bufio.NewReader(file)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				file.Close()
				time.Sleep(500 * time.Millisecond)
				break
			}
			if len(line) > 1 {
				w.emit(LogEntry{
					Time:    time.Now().Format("15:04:05"),
					Content: line[:len(line)-1],
					Stream:  stream,
				})
			}
		}
	}
}

func (w *LogWatcher) ReadRecent(logPath string, lines int) ([]LogEntry, error) {
	file, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var result []LogEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 {
			ts := parseLogTimestamp(line)
			result = append(result, LogEntry{
				Time:    ts,
				Content: line,
				Stream:  "out",
			})
		}
	}
	if len(result) > lines {
		result = result[len(result)-lines:]
	}
	return result, scanner.Err()
}

var tsPattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2})`)

func parseLogTimestamp(line string) string {
	if m := tsPattern.FindStringSubmatch(line); m != nil {
		if t, err := time.Parse("2006-01-02T15:04:05", strings.Replace(m[1], " ", "T", 1)); err == nil {
			return t.Format("15:04:05")
		}
	}
	return time.Now().Format("15:04:05")
}
