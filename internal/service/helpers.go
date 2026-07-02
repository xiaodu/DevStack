package service

import (
	"regexp"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/process"
)

type ServiceHandler interface {
	Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error
	Stop(s *ServiceInstance, pm *process.Manager) error
	Configure(s *ServiceInstance, cfg *config.Config) error
	SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error
}

var handlers = map[string]ServiceHandler{
	"nginx":  &NginxHandler{},
	"mysql":  &MySQLHandler{},
	"redis":  &RedisHandler{},
	"php":    &PHPHandler{},
	"node":   &NodeHandler{},
	"python": &PythonHandler{},
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
	if re.MatchString(content) {
		return re.ReplaceAllString(content, key+" "+value)
	}
	return content + "\n" + key + " " + value
}
