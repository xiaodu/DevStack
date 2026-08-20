package orchestrator

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcfileEntry Procfile 条目
//
// 对应 Procfile 中的一行定义，描述一个服务的完整配置。
type ProcfileEntry struct {
	Name     string            // 服务名称
	Command  string            // 可执行文件路径
	Args     []string          // 命令行参数
	Env      map[string]string // 环境变量
	WorkDir  string            // 工作目录
	AutoRun  bool              // 是否自动启动
	GraceSec int               // 优雅关闭等待秒数，默认 5
}

// Procfile Procfile 定义
//
// 包含所有服务条目和 Procfile 路径信息。
type Procfile struct {
	Path    string           // Procfile 文件路径
	Entries []ProcfileEntry  // 服务条目列表
}

// ProcfileParser Procfile 解析器
type ProcfileParser struct {
	// BaseDir Procfile 所在目录，用于解析相对路径
	BaseDir string
}

// NewProcfileParser 创建 Procfile 解析器
func NewProcfileParser(path string) (*ProcfileParser, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("获取 Procfile 绝对路径失败: %w", err)
	}

	return &ProcfileParser{
		BaseDir: filepath.Dir(absPath),
	}, nil
}

// Parse 解析 Procfile 文件
//
// 格式说明：
//   - 支持 ; 和 # 作为注释前缀
//   - 空行被忽略
//   - 每行格式：; Name: [autorun] COMMAND [ARGS...] [KEY=VALUE...] [pwd=WORKDIR] [grace=N]
//
// 示例：
//
//	; Nginx: nginx.exe -p C:\nginx
//	; MySQL: mysqld.exe --defaults-file=my.ini
//	; PHP: php-cgi.exe -b 127.0.0.1:9000
//	; Redis: redis-server.exe
//	; Node: autorun node start PORT=3000 PWD=C:\www\myapp
//	; Worker: python worker.py grace=10
func (p *ProcfileParser) Parse(path string) (*Procfile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 Procfile 失败: %w", err)
	}
	defer file.Close()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("获取 Procfile 绝对路径失败: %w", err)
	}

	procfile := &Procfile{
		Path: absPath,
	}

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行
		if line == "" {
			continue
		}

		// 跳过 # 注释行
		if strings.HasPrefix(line, "#") {
			continue
		}

		// 处理 ; 开头的行
		if strings.HasPrefix(line, ";") {
			// 去掉前导分号和空格
			trimmed := strings.TrimSpace(line[1:])

			// 纯注释：分号后没有内容，或不含冒号
			if trimmed == "" || !strings.Contains(trimmed, ":") {
				continue
			}

			// 注释行：冒号前包含空格（如 "; 格式：xxx"），不是服务定义
			colonIdx := strings.Index(trimmed, ":")
			namePart := strings.TrimSpace(trimmed[:colonIdx])
			if strings.ContainsAny(namePart, " \t") {
				continue
			}

			// 服务定义行（; Name: command），去掉前导分号后继续解析
			line = trimmed
		}

		entry, err := parseEntry(line, p.BaseDir)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行解析失败: %w", lineNum, err)
		}

		// 检查服务名是否重复
		for _, existing := range procfile.Entries {
			if existing.Name == entry.Name {
				return nil, fmt.Errorf("第 %d 行: 服务名 '%s' 重复", lineNum, entry.Name)
			}
		}

		procfile.Entries = append(procfile.Entries, *entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取 Procfile 失败: %w", err)
	}

	return procfile, nil
}

// parseEntry 解析单个条目
//
// 格式：; Name: [autorun] COMMAND [ARGS...] [KEY=VALUE...] [pwd=WORKDIR] [grace=N]
//
// 解析顺序：
//  1. 提取服务名（冒号前的部分）
//  2. 检查 autorun 前缀
//  3. 提取 pwd= 参数
//  4. 提取 grace= 参数
//  5. 提取 KEY=VALUE 环境变量
//  6. 剩余部分作为命令和参数
func parseEntry(line string, baseDir string) (*ProcfileEntry, error) {
	// 1. 找到冒号分隔
	colonIdx := strings.Index(line, ":")
	if colonIdx == -1 {
		return nil, fmt.Errorf("格式错误，缺少冒号: %s", line)
	}

	name := strings.TrimSpace(line[:colonIdx])
	if name == "" {
		return nil, fmt.Errorf("服务名不能为空")
	}

	// 去除前导分号或井号（支持 ; Name: 或 # Name: 格式）
	name = strings.TrimLeft(name, ";# \t")
	if name == "" {
		return nil, fmt.Errorf("服务名不能为空")
	}

	rest := strings.TrimSpace(line[colonIdx+1:])
	if rest == "" {
		return nil, fmt.Errorf("服务 '%s' 缺少命令定义", name)
	}

	entry := &ProcfileEntry{
		Name:     name,
		Env:      make(map[string]string),
		GraceSec: 5, // 默认 5 秒
	}

	// 2. 检查 autorun
	if strings.HasPrefix(strings.ToLower(rest), "autorun ") {
		entry.AutoRun = true
		rest = strings.TrimSpace(rest[8:])
	} else if strings.ToLower(rest) == "autorun" {
		entry.AutoRun = true
		rest = ""
	}

	// 3. 提取 pwd= 参数
	rest = extractPwdParam(rest, baseDir, entry)

	// 4. 提取 grace= 参数
	rest = extractGraceParam(rest, entry)

	// 5. 提取 KEY=VALUE 环境变量和命令
	parts := splitParts(rest)
	var cmdParts []string

	for _, part := range parts {
		if kv, ok := parseKeyValue(part); ok {
			// 这是 KEY=VALUE 环境变量
			entry.Env[kv[0]] = kv[1]
		} else {
			// 这是命令或参数
			cmdParts = append(cmdParts, part)
		}
	}

	// 6. 解析命令
	if len(cmdParts) == 0 {
		return nil, fmt.Errorf("服务 '%s' 缺少命令", name)
	}

	entry.Command = cmdParts[0]
	if len(cmdParts) > 1 {
		entry.Args = cmdParts[1:]
	}

	return entry, nil
}

// extractPwdParam 提取 pwd= 参数
//
// 支持格式：
//   - pwd=C:\path\to\dir
//   - PWD=C:\path\to\dir
//   - pwd="C:\path with spaces"
//
// 相对路径会被转换为绝对路径（相对于 Procfile 所在目录）。
func extractPwdParam(rest string, baseDir string, entry *ProcfileEntry) string {
	lower := strings.ToLower(rest)
	idx := strings.Index(lower, "pwd=")
	if idx == -1 {
		return rest
	}

	// 提取 pwd= 后面的值
	start := idx + 4
	remaining := rest[start:]

	// 计算实际消耗的长度
	consumed := 0
	var value string

	remaining = strings.TrimSpace(remaining)
	if len(remaining) == 0 {
		return rest
	}

	// 带引号
	if remaining[0] == '"' || remaining[0] == '\'' {
		quote := remaining[0]
		endIdx := strings.IndexByte(remaining[1:], quote)
		if endIdx == -1 {
			value = remaining[1:]
			consumed = len(remaining)
		} else {
			value = remaining[1 : endIdx+1]
			consumed = endIdx + 2 // 包含两个引号
		}
	} else {
		// 不带引号，取到下一个空格
		spaceIdx := strings.IndexByte(remaining, ' ')
		if spaceIdx == -1 {
			value = remaining
			consumed = len(remaining)
		} else {
			value = remaining[:spaceIdx]
			consumed = spaceIdx
		}
	}

	// 转换为绝对路径
	if !filepath.IsAbs(value) {
		value = filepath.Join(baseDir, value)
	}
	entry.WorkDir = value

	// 从 rest 中移除 pwd= 部分
	// idx 是 pwd 在 rest 中的位置，start 是 pwd= 后面的位置
	// 实际消耗的字符数需要考虑 TrimSpace 的影响
	newRest := rest[:idx] + rest[start+consumed:]
	return strings.TrimSpace(newRest)
}

// extractGraceParam 提取 grace= 参数
//
// 格式：grace=N（N 为秒数）
func extractGraceParam(rest string, entry *ProcfileEntry) string {
	lower := strings.ToLower(rest)
	idx := strings.Index(lower, "grace=")
	if idx == -1 {
		return rest
	}

	start := idx + 6
	remaining := rest[start:]

	// 计算实际消耗的长度
	consumed := 0
	var value string

	remaining = strings.TrimSpace(remaining)
	if len(remaining) == 0 {
		return rest
	}

	// 带引号
	if remaining[0] == '"' || remaining[0] == '\'' {
		quote := remaining[0]
		endIdx := strings.IndexByte(remaining[1:], quote)
		if endIdx == -1 {
			value = remaining[1:]
			consumed = len(remaining)
		} else {
			value = remaining[1 : endIdx+1]
			consumed = endIdx + 2
		}
	} else {
		// 不带引号，取到下一个空格
		spaceIdx := strings.IndexByte(remaining, ' ')
		if spaceIdx == -1 {
			value = remaining
			consumed = len(remaining)
		} else {
			value = remaining[:spaceIdx]
			consumed = spaceIdx
		}
	}

	if n, err := parseGraceSeconds(value); err == nil && n > 0 {
		entry.GraceSec = n
	}

	// 从 rest 中移除 grace= 部分
	newRest := rest[:idx] + rest[start+consumed:]
	return strings.TrimSpace(newRest)
}

// parseGraceSeconds 解析优雅关闭秒数
func parseGraceSeconds(s string) (int, error) {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid grace value: %s", s)
		}
		n = n*10 + int(ch-'0')
	}
	if n <= 0 {
		return 0, fmt.Errorf("grace value must be positive: %d", n)
	}
	return n, nil
}

// extractQuotedOrWord 从字符串开头提取带引号的值或单个单词
//
// 带引号：提取引号内的内容
// 不带引号：提取到下一个空格
func extractQuotedOrWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	// 带引号
	if s[0] == '"' || s[0] == '\'' {
		quote := s[0]
		endIdx := strings.IndexByte(s[1:], quote)
		if endIdx == -1 {
			// 没有找到闭合引号，取到末尾
			return s[1:]
		}
		return s[1 : endIdx+1]
	}

	// 不带引号，取到下一个空格
	idx := strings.IndexByte(s, ' ')
	if idx == -1 {
		return s
	}
	return s[:idx]
}

// splitParts 分割命令行字符串，处理引号
//
// 例如：nginx.exe -p "C:\Program Files\nginx" --key=value
// 分割为：["nginx.exe", "-p", "C:\Program Files\nginx", "--key=value"]
func splitParts(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		ch := s[i]

		switch {
		case inQuote:
			if ch == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(ch)
			}
		case ch == '"' || ch == '\'':
			inQuote = true
			quoteChar = ch
		case ch == ' ' || ch == '\t':
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// parseKeyValue 解析 KEY=VALUE 格式的字符串
//
// 返回：
//   - [key, value]: 成功解析
//   - nil, false: 不是 KEY=VALUE 格式
func parseKeyValue(s string) ([2]string, bool) {
	// 必须包含 = 且 = 不在开头
	eqIdx := strings.IndexByte(s, '=')
	if eqIdx <= 0 {
		return [2]string{}, false
	}

	key := s[:eqIdx]
	value := s[eqIdx+1:]

	// 检查 key 是否合法（字母、数字、下划线）
	for _, ch := range key {
		if !isAlphaNumOrUnderscore(ch) {
			return [2]string{}, false
		}
	}

	return [2]string{key, value}, true
}

// isAlphaNumOrUnderscore 检查字符是否为字母、数字或下划线
func isAlphaNumOrUnderscore(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_'
}

// WriteProcfile 将 Procfile 写入文件
//
// 用于生成默认的 Procfile 模板。
func WriteProcfile(path string, entries []ProcfileEntry) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建 Procfile 失败: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	// 写入文件头（使用 # 作为纯注释前缀，避免被解析为服务定义）
	writer.WriteString("# DevStack Procfile\n")
	writer.WriteString("# 格式：; Name: [autorun] COMMAND [ARGS...] [KEY=VALUE...] [pwd=WORKDIR] [grace=N]\n")
	writer.WriteString("#\n")
	writer.WriteString("# 示例：\n")
	writer.WriteString("# ; Nginx: nginx.exe -p C:\\nginx\n")
	writer.WriteString("# ; MySQL: mysqld.exe --defaults-file=my.ini\n")
	writer.WriteString("# ; PHP: php-cgi.exe -b 127.0.0.1:9000\n")
	writer.WriteString("# ; Redis: redis-server.exe\n")
	writer.WriteString("# ; Node: autorun node start PORT=3000 PWD=C:\\www\\myapp\n")
	writer.WriteString("\n")

	for _, entry := range entries {
		line := formatEntry(entry)
		writer.WriteString(line)
		writer.WriteString("\n")
	}

	return nil
}

// formatEntry 格式化 Procfile 条目
func formatEntry(entry ProcfileEntry) string {
	var sb strings.Builder

	// 服务名
	sb.WriteString("; ")
	sb.WriteString(entry.Name)
	sb.WriteString(": ")

	// autorun
	if entry.AutoRun {
		sb.WriteString("autorun ")
	}

	// 命令
	sb.WriteString(entry.Command)

	// 参数
	for _, arg := range entry.Args {
		sb.WriteString(" ")
		sb.WriteString(arg)
	}

	// 环境变量
	for k, v := range entry.Env {
		sb.WriteString(" ")
		sb.WriteString(k)
		sb.WriteString("=")
		if strings.Contains(v, " ") {
			sb.WriteString("\"")
			sb.WriteString(v)
			sb.WriteString("\"")
		} else {
			sb.WriteString(v)
		}
	}

	// 工作目录
	if entry.WorkDir != "" {
		sb.WriteString(" pwd=")
		sb.WriteString(entry.WorkDir)
	}

	// 优雅关闭秒数（非默认值时）
	if entry.GraceSec != 5 && entry.GraceSec > 0 {
		sb.WriteString(" grace=")
		sb.WriteString(strconv.Itoa(entry.GraceSec))
	}

	return sb.String()
}

// GenerateDefaultProcfile 生成默认的 Procfile 模板
//
// 基于已安装的工具生成默认配置。
func GenerateDefaultProcfile(nginxPath, mysqlPath, phpPath, redisPath string) []ProcfileEntry {
	var entries []ProcfileEntry

	if nginxPath != "" {
		entries = append(entries, ProcfileEntry{
			Name:     "Nginx",
			Command:  filepath.Join(nginxPath, "nginx.exe"),
			Args:     []string{"-p", nginxPath},
			GraceSec: 5,
		})
	}

	if mysqlPath != "" {
		entries = append(entries, ProcfileEntry{
			Name:     "MySQL",
			Command:  filepath.Join(mysqlPath, "bin", "mysqld.exe"),
			Args:     []string{"--defaults-file=" + filepath.Join(mysqlPath, "my.ini")},
			GraceSec: 10,
		})
	}

	if phpPath != "" {
		entries = append(entries, ProcfileEntry{
			Name:     "PHP",
			Command:  filepath.Join(phpPath, "php-cgi.exe"),
			Args:     []string{"-b", "127.0.0.1:9000"},
			GraceSec: 3,
		})
	}

	if redisPath != "" {
		entries = append(entries, ProcfileEntry{
			Name:     "Redis",
			Command:  filepath.Join(redisPath, "redis-server.exe"),
			GraceSec: 3,
		})
	}

	return entries
}
