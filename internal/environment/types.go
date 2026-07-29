package environment

import (
	"time"
)

// EnvironmentProfile 是一个可复用的"环境"模板实体，对应核心概念：
// 一套特定版本的软件组合，如 "PHP7.4 + MySQL5.7 + Nginx1.20"，
// 可被多个项目共享引用，也支持预设或用户自定义保存。
type EnvironmentProfile struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	IsBuiltin   bool                   `json:"isBuiltin"`
	IsCustom    bool                   `json:"isCustom"`
	CreatedAt   string                 `json:"createdAt"`
	UpdatedAt   string                 `json:"updatedAt"`

	// Services 声明环境需要的服务以及对应版本
	Services map[string]EnvironmentServiceSpec `json:"services,omitempty"`
	// Runtimes 声明环境需要的运行时以及对应版本
	Runtimes map[string]EnvironmentRuntimeSpec `json:"runtimes,omitempty"`
	// Dependencies 显式自定义依赖 capability -> providerID
	Dependencies map[string]string `json:"dependencies,omitempty"`
	// Tags 便于筛选：如 "LAMP"、"生产友好"、"高性能" 等
	Tags []string `json:"tags,omitempty"`
}

type EnvironmentServiceSpec struct {
	// ID 是服务 ID，比如 "mysql"、"nginx"、"redis"
	ID string `json:"id"`
	// 期望的版本号（语义化或安装包里存在的版本字符串）
	Version string `json:"version"`
}

type EnvironmentRuntimeSpec struct {
	// RuntimeID 是运行时 ID，比如 "php"、"node"、"python"
	RuntimeID string `json:"runtimeId"`
	// 期望的版本号，例如 "7.4"、"8.3"、"20"
	Version string `json:"version"`
	// 是否设置为该语言运行时的默认激活版本
	Default bool `json:"default,omitempty"`
}

// ProjectCount 用于 UI 展示"被多少项目共享"，不持久化，运行时聚合。
type ProfileUsage struct {
	ProjectCount int `json:"projectCount"`
}

// NewID 生成 profile ID，按时间戳前缀保证排序友好
func NewID() string {
	return "env_" + time.Now().Format("20060102150405") + "_" + randSuffix()
}

func randSuffix() string {
	s := make([]byte, 6)
	for i := range s {
		s[i] = "0123456789abcdefghijklmnopqrstuvwxyz"[time.Now().UnixNano()%36]
		time.Sleep(time.Nanosecond)
	}
	return string(s)
}
