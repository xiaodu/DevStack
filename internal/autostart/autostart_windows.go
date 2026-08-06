package autostart

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "DevStack"
)

// Enable 添加开机自启项：写入当前用户 HKCU Run 键（无需管理员权限）。
func Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	return enableRegistry(exe)
}

// Disable 移除开机自启项。值不存在时视为已关闭，不报错。
func Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	if _, _, err := k.GetStringValue(valueName); err != nil {
		return nil
	}
	return k.DeleteValue(valueName)
}

// IsEnabled 返回当前是否已注册开机自启。
func IsEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(valueName)
	return err == nil
}

// Sync 将注册表状态与期望状态对齐。
func Sync(enabled bool) error {
	if enabled == IsEnabled() {
		return nil
	}
	if enabled {
		return Enable()
	}
	return Disable()
}

func enableRegistry(exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开注册表 Run 键失败: %w", err)
	}
	defer k.Close()

	cmdLine := `"` + exe + `"`
	if existing, _, _ := k.GetStringValue(valueName); existing == cmdLine {
		return nil
	}
	return k.SetStringValue(valueName, cmdLine)
}
