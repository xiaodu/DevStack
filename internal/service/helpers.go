package service

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

const phpWorkerCount = 4

type BaseHandler struct{}

func (h *BaseHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
	if s.process == nil || s.process.pid <= 0 {
		return nil
	}
	s.Info.Status = core.StatusStopping
	pid := s.process.pid
	pm.Stop(pid)
	for i := 0; i < 6; i++ {
		if !pm.IsRunning(pid) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	s.process = nil
	s.Info.Status = core.StatusStopped
	s.Info.PID = 0
	return nil
}

type ServiceHandler interface {
	Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error
	Stop(s *ServiceInstance, pm *process.Manager) error
	Configure(s *ServiceInstance, cfg *config.Config) error
	SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error
}

var handlers = map[string]ServiceHandler{
	"nginx": &NginxHandler{},
	"mysql": &MySQLHandler{},
	"redis": &RedisHandler{},
	"php":   &PHPHandler{},
}

func now() time.Time {
	return time.Now()
}

func updateIniValue(content, key, value string) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*[=:].*$`)
	if re.MatchString(content) {
		return re.ReplaceAllString(content, key+" = "+value)
	}
	return content + "\n" + key + " = " + value
}

func updateConfValue(content, key, value string) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s+.*$`)
	repl := key + " " + value
	if !strings.HasSuffix(strings.TrimSpace(repl), ";") {
		repl += ";"
	}
	if re.MatchString(content) {
		return re.ReplaceAllString(content, repl)
	}
	return content + "\n" + repl
}

func generatePHPUpstreamConfig(basePort int) string {
	var buf strings.Builder
	buf.WriteString("upstream php_backend {\n")
	for i := 0; i < phpWorkerCount; i++ {
		fmt.Fprintf(&buf, "\tserver 127.0.0.1:%d;\n", basePort+i)
	}
	buf.WriteString("}")
	return buf.String()
}
