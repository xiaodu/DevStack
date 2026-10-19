package service

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/registry"
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

// defaultVersionOf 返回注册表里该组件的默认版本，找不到返回空串。
func defaultVersionOf(kind string) string {
	if pkg := registry.Builtin().Get(kind); pkg != nil {
		return pkg.DefaultVersion
	}
	return ""
}

// versionGreater 按数字段逐段比较版本号。
// 不能用字符串序：字典序下 "3.9.13" 会被判为高于 "3.12.8"，多版本共存时选错目标。
func versionGreater(a, b string) bool {
	as, bs := versionSegments(a), versionSegments(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// versionSegments 提取 "8.0.36"、"1.26.2-1" 这类版本号里的数字段，非数字部分丢弃。
func versionSegments(v string) []int {
	var out []int
	for _, seg := range strings.FieldsFunc(v, func(r rune) bool {
		return r < '0' || r > '9'
	}) {
		n, err := strconv.Atoi(seg)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
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
