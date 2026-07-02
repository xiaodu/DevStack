package site

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func hostsFilePath() string {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	return filepath.Join(systemRoot, "System32", "drivers", "etc", "hosts")
}

func ensureHostsEntry(domain string, add bool) error {
	if domain == "" || domain == "localhost" {
		return nil
	}
	path := hostsFilePath()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 hosts 文件失败: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	entry := fmt.Sprintf("127.0.0.1 %s", strings.TrimSpace(domain))
	var newLines []string
	found := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == entry {
			found = true
			if !add {
				continue
			}
		}
		newLines = append(newLines, line)
	}

	if add && !found {
		newLines = append(newLines, entry)
	}

	newContent := strings.Join(newLines, "\n")
	// Restore Windows line endings
	newContent = strings.ReplaceAll(newContent, "\n", "\r\n")
	if newContent != string(data) {
		if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
			return fmt.Errorf("写入 hosts 文件失败（可能需管理员权限）: %w", err)
		}
	}
	return nil
}
