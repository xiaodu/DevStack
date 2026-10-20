// Package installdir 提供安装目录迁移的预检与文件复制能力。
//
// 迁移只负责把目录内容安全复制到新位置，并在复制后重写配置文件中出现的
// 旧安装目录绝对路径；配置切换、管理器重建与旧目录清理由调用方完成。
package installdir

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xiaodu/DevStack/internal/core"
)

const maxRewriteSize = 8 << 20

// ProgressFunc 报告迁移进度。copied/total 以字节计，total 为 0 时表示未知。
type ProgressFunc func(phase, message string, copied, total int64)

// Check 校验一次安装目录迁移是否可行，并收集源目录大小与目标目录状态。
func Check(currentDir, targetDir string) core.InstallDirCheck {
	check := core.InstallDirCheck{
		CurrentDir: currentDir,
		TargetDir:  strings.TrimSpace(targetDir),
		Errors:     []string{},
		Warnings:   []string{},
	}
	addErr := func(msg string) { check.Errors = append(check.Errors, msg) }
	addWarn := func(msg string) { check.Warnings = append(check.Warnings, msg) }

	current := strings.TrimSpace(currentDir)
	if current == "" {
		addErr("当前安装目录为空，请先完成初始化设置")
	} else {
		current = filepath.Clean(current)
		check.CurrentDir = current
		info, err := os.Stat(current)
		switch {
		case err != nil:
			addErr("当前安装目录不可访问: " + err.Error())
		case !info.IsDir():
			addErr("当前安装目录不是文件夹")
		default:
			size, count, err := dirSize(current)
			if err != nil {
				addWarn("统计源目录大小时有文件无法读取: " + err.Error())
			}
			check.SourceSize = size
			check.FileCount = count
		}
	}

	target := check.TargetDir
	if target == "" {
		addErr("请选择新的安装目录")
	} else if !filepath.IsAbs(target) {
		addErr("请填写绝对路径，例如 D:\\DevStack")
	} else {
		target = filepath.Clean(target)
		check.TargetDir = target

		if isVolumeRoot(target) {
			addErr("不能直接使用盘符根目录，请选择一个专用文件夹")
		}
		if current != "" {
			if strings.EqualFold(target, current) {
				addErr("新目录与当前安装目录相同")
			} else if isSubPath(current, target) {
				addErr("新目录不能位于当前安装目录内部")
			} else if isSubPath(target, current) {
				addErr("新目录不能是当前安装目录的上级目录")
			}
		}

		info, err := os.Stat(target)
		switch {
		case err == nil && !info.IsDir():
			check.TargetExists = true
			addErr("目标路径已存在且不是文件夹")
		case err == nil:
			check.TargetExists = true
			entries, readErr := os.ReadDir(target)
			if readErr != nil {
				addErr("目标目录不可读取: " + readErr.Error())
			} else {
				check.TargetEmpty = len(entries) == 0
				if !check.TargetEmpty {
					addErr(fmt.Sprintf("目标目录非空（已有 %d 个条目），请选择空目录", len(entries)))
				}
			}
		case !os.IsNotExist(err):
			addErr("目标目录不可访问: " + err.Error())
		}
	}

	check.Valid = len(check.Errors) == 0
	return check
}

// Migrate 将 currentDir 的完整内容复制到 targetDir，并在副本中重写旧目录绝对路径。
//
// 复制先写入目标同级的临时目录，全部成功后才重命名为目标目录，
// 避免失败时留下半成品或覆盖目标目录。调用前必须确保所有组件已停止。
func Migrate(currentDir, targetDir string, progress ProgressFunc) ([]string, error) {
	check := Check(currentDir, targetDir)
	if !check.Valid {
		return nil, errors.New(strings.Join(check.Errors, "；"))
	}

	current := check.CurrentDir
	target := check.TargetDir
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("创建目标目录的上级目录失败: %w", err)
	}

	staging, err := os.MkdirTemp(parent, ".devstack-migrate-*")
	if err != nil {
		return nil, fmt.Errorf("创建迁移临时目录失败: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()

	if progress != nil {
		progress("copying", "正在复制安装目录…", 0, check.SourceSize)
	}
	warnings, err := copyTree(current, staging, check.SourceSize, progress)
	if err != nil {
		return nil, err
	}

	if progress != nil {
		progress("rewriting", "正在重写配置文件中的安装路径…", check.SourceSize, check.SourceSize)
	}
	rewriteWarnings, err := rewriteTree(staging, current, target)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, rewriteWarnings...)

	if info, statErr := os.Stat(target); statErr == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("目标路径已存在且不是文件夹: %s", target)
		}
		entries, readErr := os.ReadDir(target)
		if readErr != nil {
			return nil, fmt.Errorf("读取目标目录失败: %w", readErr)
		}
		if len(entries) > 0 {
			return nil, fmt.Errorf("目标目录在迁移过程中变为非空，已中止: %s", target)
		}
		if err := os.Remove(target); err != nil {
			return nil, fmt.Errorf("移除空的目标目录失败: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("检查目标目录失败: %w", statErr)
	}

	if err := os.Rename(staging, target); err != nil {
		return nil, fmt.Errorf("将迁移结果移动到目标目录失败: %w", err)
	}
	committed = true
	return warnings, nil
}

func copyTree(src, dst string, total int64, progress ProgressFunc) ([]string, error) {
	var copied int64
	var warnings []string
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("读取 %s 信息失败: %w", rel, err)
		}

		if entry.IsDir() {
			return os.MkdirAll(out, info.Mode().Perm()|0o200)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			warnings = append(warnings, "已跳过符号链接: "+rel)
			return nil
		}
		if !info.Mode().IsRegular() {
			warnings = append(warnings, "已跳过非普通文件: "+rel)
			return nil
		}
		if err := copyFile(path, out, info); err != nil {
			return fmt.Errorf("复制 %s 失败: %w", rel, err)
		}
		copied += info.Size()
		if progress != nil {
			progress("copying", "正在复制 "+rel, copied, total)
		}
		return nil
	})
	return warnings, err
}

func copyFile(src, dst string, info fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// rewriteTree 只处理配置文件，跳过 data/www 等用户数据目录，
// 避免误改数据库文件或站点源码中的同名字符串。
func rewriteTree(root, oldDir, newDir string) ([]string, error) {
	pairs := buildRewritePairs(oldDir, newDir)
	if len(pairs) == 0 {
		return nil, nil
	}
	var warnings []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if filepath.Dir(rel) == "." {
				switch entry.Name() {
				case "data", "www", "logs":
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !entry.Type().IsRegular() || !rewritable(rel) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxRewriteSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, "无法读取配置文件，已保持原样: "+rel)
			return nil
		}
		changed := false
		for _, pair := range pairs {
			var replaced bool
			data, replaced = pair.replace(data)
			changed = changed || replaced
		}
		if !changed {
			return nil
		}
		if err := os.WriteFile(path, data, info.Mode().Perm()); err != nil {
			return fmt.Errorf("重写配置文件 %s 失败: %w", rel, err)
		}
		return nil
	})
	return warnings, err
}

type rewritePair struct {
	re   *regexp.Regexp
	repl string
}

func (p rewritePair) replace(data []byte) ([]byte, bool) {
	if !p.re.Match(data) {
		return data, false
	}
	return p.re.ReplaceAllFunc(data, func([]byte) []byte { return []byte(p.repl) }), true
}

func buildRewritePairs(oldDir, newDir string) []rewritePair {
	oldDir = filepath.Clean(oldDir)
	newDir = filepath.Clean(newDir)
	type variant struct{ old, replacement string }
	variants := []variant{
		// JSON/TOML 中的反斜杠会被转义，必须先替换双反斜杠形式。
		{strings.ReplaceAll(oldDir, `\`, `\\`), strings.ReplaceAll(newDir, `\`, `\\`)},
		{filepath.ToSlash(oldDir), filepath.ToSlash(newDir)},
		{oldDir, newDir},
	}
	seen := make(map[string]bool, len(variants))
	pairs := make([]rewritePair, 0, len(variants))
	for _, v := range variants {
		if v.old == "" || seen[v.old] {
			continue
		}
		seen[v.old] = true
		re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(v.old))
		if err != nil {
			continue
		}
		pairs = append(pairs, rewritePair{re: re, repl: v.replacement})
	}
	return pairs
}

func rewritable(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	if base == "procfile" {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".conf", ".ini", ".json", ".toml", ".yaml", ".yml", ".xml", ".txt",
		".bat", ".cmd", ".ps1", ".properties", ".env":
		return true
	}
	return false
}

func dirSize(root string) (int64, int64, error) {
	var size, count int64
	var firstErr error
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if firstErr == nil {
				firstErr = walkErr
			}
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		size += info.Size()
		count++
		return nil
	})
	if err != nil {
		return size, count, err
	}
	return size, count, firstErr
}

func isSubPath(parent, child string) bool {
	parent = strings.ToLower(filepath.Clean(parent))
	child = strings.ToLower(filepath.Clean(child))
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func isVolumeRoot(path string) bool {
	clean := filepath.Clean(path)
	return filepath.Dir(clean) == clean
}
