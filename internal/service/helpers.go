package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/resource"
)

const phpWorkerCount = 4

// ConfigHandler 仅用于配置操作，所有启停逻辑已迁移到 Provider 架构
type ConfigHandler interface {
	Configure(s *ServiceInstance, cfg *config.Config, resMgr *resource.Manager) error
	SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error
}

var configHandlers = map[string]ConfigHandler{
	"nginx": &NginxHandler{},
	"mysql": &MySQLHandler{},
	"redis": &RedisHandler{},
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
