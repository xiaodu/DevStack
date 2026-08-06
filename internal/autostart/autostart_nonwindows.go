//go:build !windows

package autostart

// Enable 非 Windows 平台不实现开机自启。
func Enable() error { return nil }

// Disable 非 Windows 平台不实现开机自启。
func Disable() error { return nil }

// IsEnabled 非 Windows 平台始终返回 false。
func IsEnabled() bool { return false }

// Sync 非 Windows 平台为无操作。
func Sync(enabled bool) error { return nil }
