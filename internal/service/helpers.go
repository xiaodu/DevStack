package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/resource"
)

const phpWorkerCount = 4

func allocatedPHPBasePorts(resMgr *resource.Manager) []int {
	if resMgr == nil {
		return nil
	}
	var ports []int
	for port, owner := range resMgr.Ports().ListAllocated() {
		if strings.HasPrefix(owner, "php:") {
			ports = append(ports, port)
		}
	}
	sort.Ints(ports)
	return ports
}

// ConfigHandler 仅用于配置操作，所有启停逻辑已迁移到 Provider 架构
type ConfigHandler interface {
	Configure(s *ServiceInstance, cfg *config.Config, resMgr *resource.Manager) error
	SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error
}

var configHandlers = map[string]ConfigHandler{
	"nginx": &NginxHandler{},
	"mysql": &MySQLHandler{},
	"redis": &RedisHandler{},
	"php":   &PHPHandler{},
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
	if re.MatchString(content) {
		return re.ReplaceAllString(content, repl)
	}
	return content + "\n" + repl
}

// boolInt 输出 my.ini 使用的 1/0 布尔值
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func generatePHPUpstreamConfig(basePorts []int) string {
	if len(basePorts) == 0 {
		basePorts = []int{9000}
	}
	var buf strings.Builder
	buf.WriteString("upstream php_backend {\n")
	for _, base := range basePorts {
		for i := 0; i < phpWorkerCount; i++ {
			fmt.Fprintf(&buf, "\tserver 127.0.0.1:%d;\n", base+i)
		}
	}
	buf.WriteString("}")
	return buf.String()
}
