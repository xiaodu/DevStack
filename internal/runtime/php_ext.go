package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xiaodu/DevStack/internal/config"
)

// GeneratePHPFPMConfig generates php-fpm.d/www.conf content (reference: 宝塔面板高并发调优)
func GeneratePHPFPMConfig(port int) string {
	return fmt.Sprintf(`[www]
listen = 127.0.0.1:%d
listen.allowed_clients = 127.0.0.1

; User & Group (Unix only)
user = www
group = www

; Process Manager - Dynamic mode (宝塔推荐)
pm = dynamic
pm.max_children = 50
pm.start_servers = 8
pm.min_spare_servers = 6
pm.max_spare_servers = 16
pm.max_requests = 300

; Request timeout (prevent zombie processes)
request_terminate_timeout = 30
request_slowlog_timeout = 2
slowlog = /var/log/php-fpm-slow.log

; Status monitoring
pm.status_path = /php-fpm-status
ping.path = /php-fpm-ping

; Performance tuning
rlimit_files = 4096
catch_workers_output = yes
decorate_workers_output = no
`, port)
}

func getExtensionList(cfg *config.Config, version string) ([]PHPExtensionInfo, error) {
	phpDir := filepath.Join(cfg.Get().InstallDir, "programs", "php", version)
	extDir := filepath.Join(phpDir, "ext")

	enabledExts := make(map[string]bool)
	phpIni := filepath.Join(phpDir, "php.ini")
	if data, err := os.ReadFile(phpIni); err == nil {
		re := extListRegex()
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			name := cleanExtName(m[1])
			enabledExts[strings.ToLower(name)] = true
		}
	}

	var extensions []PHPExtensionInfo
	entries, err := os.ReadDir(extDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".dll") && !strings.HasSuffix(name, ".so") {
			continue
		}
		extName := cleanExtName(name)
		extensions = append(extensions, PHPExtensionInfo{
			Name:    extName,
			Enabled: enabledExts[strings.ToLower(extName)],
		})
	}
	return extensions, nil
}

// zendExtensions 是需要通过 zend_extension= 加载的扩展（opcache、xdebug 等），
// 直接用 extension= 加载不会生效。
var zendExtensions = map[string]bool{"opcache": true, "xdebug": true}

func isZendExtension(name string) bool {
	return zendExtensions[strings.ToLower(cleanExtName(name))]
}

// extListRegex 匹配 php.ini 中启用的 extension= 与 zend_extension= 行。
func extListRegex() *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[ \t]*;?[ \t]*(?:zend_)?extension[ \t]*=[ \t]*(\S+)`)
}

// extLineRegex 匹配 php.ini 中所有 extension=/zend_extension= 行（含被注释掉的）。
func extLineRegex() *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[ \t]*;?[ \t]*(?:zend_)?extension[ \t]*=[ \t]*\S+.*$`)
}

// LoadEnabledExtensionNames 读取现有 php.ini 中已启用的扩展名（含 zend_extension），
// 供重新生成 php.ini 后回填，避免配置变更清空用户扩展选择。
func LoadEnabledExtensionNames(phpIniPath string) []string {
	data, err := os.ReadFile(phpIniPath)
	if err != nil {
		return nil
	}
	var names []string
	seen := make(map[string]bool)
	for _, m := range extListRegex().FindAllStringSubmatch(string(data), -1) {
		name := cleanExtName(m[1])
		key := strings.ToLower(name)
		if !seen[key] {
			seen[key] = true
			names = append(names, name)
		}
	}
	return names
}

func extensionFileFor(phpDir, name string) (string, bool) {
	extDir := filepath.Join(phpDir, "ext")
	entries, err := os.ReadDir(extDir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fname := entry.Name()
		if !strings.HasSuffix(fname, ".dll") && !strings.HasSuffix(fname, ".so") {
			continue
		}
		if cleanExtName(fname) == name {
			return fname, true
		}
	}
	return "", false
}

// writeExtensionSection 在给定 php.ini 内容上重建扩展段，返回写入后的完整内容。
func writeExtensionSection(content, phpDir, version string, enabledNames []string) (string, error) {
	extDir := filepath.Join(phpDir, "ext")
	content = extLineRegex().ReplaceAllString(content, "")
	content = regexp.MustCompile(`(?m)^[ \t]*;?\s*extension_dir\s*=.*$`).ReplaceAllString(content, "")
	extDirLine := fmt.Sprintf(`extension_dir = "%s"`, filepath.ToSlash(extDir))

	builtExtensions := make(map[string]string)
	if entries, err := os.ReadDir(extDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			fname := entry.Name()
			if strings.HasSuffix(fname, ".dll") || strings.HasSuffix(fname, ".so") {
				builtExtensions[cleanExtName(fname)] = fname
			}
		}
	}

	for _, name := range enabledNames {
		if _, ok := builtExtensions[name]; !ok {
			if _, ok2 := builtExtensions["php_"+name]; ok2 {
				continue
			}
			return "", fmt.Errorf("扩展 %s 未在 ext/ 目录中找到对应的 .dll 文件", name)
		}
	}

	usePrefix := !phpVersionAtLeast(version, 7, 2)
	content = strings.TrimSpace(content)
	content += "\n" + extDirLine + "\n"
	for _, name := range enabledNames {
		lineName := name
		if usePrefix {
			if f, ok := builtExtensions[name]; ok {
				lineName = f
			}
		}
		kw := "extension"
		if isZendExtension(name) {
			kw = "zend_extension"
		}
		content += kw + " = " + lineName + "\n"
	}
	return content, nil
}

// RegeneratePHPIni 重新生成 php.ini 基础配置并保留已启用的扩展，供配置保存时调用。
func RegeneratePHPIni(cfg *config.Config, phpDir, version string) error {
	phpIni := filepath.Join(phpDir, "php.ini")
	enabled := LoadEnabledExtensionNames(phpIni)
	base := GeneratePHPIniContent(cfg, phpDir, version)
	content, err := writeExtensionSection(base, phpDir, version, enabled)
	if err != nil {
		return err
	}
	return os.WriteFile(phpIni, []byte(content), 0644)
}

func saveExtensionList(cfg *config.Config, version string, extensionNames []string) error {
	phpDir := filepath.Join(cfg.Get().InstallDir, "programs", "php", version)
	phpIni := filepath.Join(phpDir, "php.ini")

	if !fileExists(phpIni) {
		os.WriteFile(phpIni, []byte(GeneratePHPIniContent(cfg, phpDir, version)), 0644)
	}

	data, err := os.ReadFile(phpIni)
	if err != nil {
		return err
	}
	content, err := writeExtensionSection(string(data), phpDir, version, extensionNames)
	if err != nil {
		return err
	}
	if err := os.WriteFile(phpIni, []byte(content), 0644); err != nil {
		return err
	}

	written, err := os.ReadFile(phpIni)
	if err != nil {
		return err
	}
	writtenStr := string(written)
	usePrefix := !phpVersionAtLeast(version, 7, 2)
	for _, name := range extensionNames {
		checkName := name
		if usePrefix {
			if f, ok := extensionFileFor(phpDir, name); ok {
				checkName = f
			}
		}
		kw := "extension"
		if isZendExtension(name) {
			kw = "zend_extension"
		}
		if !strings.Contains(writtenStr, kw+" = "+checkName) {
			return fmt.Errorf("写入验证失败：php.ini 中未找到 %s = %s", kw, checkName)
		}
	}

	return nil
}

func cleanExtName(raw string) string {
	name := strings.TrimSuffix(strings.TrimSuffix(raw, ".dll"), ".so")
	name = strings.TrimPrefix(name, "php_")
	return name
}

func phpVersionAtLeast(version string, major, minor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	var vMajor, vMinor int
	fmt.Sscanf(parts[0], "%d", &vMajor)
	fmt.Sscanf(parts[1], "%d", &vMinor)
	if vMajor > major {
		return true
	}
	if vMajor == major && vMinor >= minor {
		return true
	}
	return false
}

func GeneratePHPIniContent(cfg *config.Config, phpDir, version string) string {
	extDir := filepath.ToSlash(filepath.Join(phpDir, "ext"))
	usePrefix := !phpVersionAtLeast(version, 7, 2)

	var buf strings.Builder
	buf.WriteString("[PHP]\n")
	buf.WriteString("engine = On\n\n")

	phpCfg := cfg.Get().PHPConfig

	displayErrors := "On"
	if !phpCfg.DisplayErrors {
		displayErrors = "Off"
	}
	logErrors := "On"
	if !phpCfg.LogErrors {
		logErrors = "Off"
	}
	fileUploads := "On"
	if !phpCfg.FileUploads {
		fileUploads = "Off"
	}
	fixPathinfo := "1"
	if !phpCfg.FixPathinfo {
		fixPathinfo = "0"
	}

	buf.WriteString("error_reporting = E_ALL & ~E_NOTICE & ~E_DEPRECATED & ~E_STRICT\n")
	buf.WriteString("display_errors = " + displayErrors + "\n")
	buf.WriteString("display_startup_errors = " + displayErrors + "\n")
	buf.WriteString("log_errors = " + logErrors + "\n")
	buf.WriteString("error_log = \"" + phpCfg.ErrorLog + "\"\n")
	buf.WriteString("file_uploads = " + fileUploads + "\n\n")

	opcacheEnable := "1"
	if !phpCfg.OPcacheEnabled {
		opcacheEnable = "0"
	}

	buf.WriteString("; OPcache - bytecode cache (performance boost 30-50%%)\n")
	buf.WriteString("opcache.enable = " + opcacheEnable + "\n")
	buf.WriteString("opcache.enable_cli = 0\n")
	buf.WriteString("opcache.memory_consumption = 384\n")
	buf.WriteString("opcache.interned_strings_buffer = 16\n")
	buf.WriteString("opcache.max_accelerated_files = 79999\n")
	buf.WriteString("opcache.max_wasted_percentage = 10\n")
	buf.WriteString("opcache.validate_permission = 1\n")
	buf.WriteString("opcache.revalidate_freq = 2\n")
	buf.WriteString("opcache.save_comments = 1\n")
	buf.WriteString("opcache.fast_shutdown = 1\n")
	buf.WriteString("opcache.optimization_level = 0x7FFFBFFF\n")
	buf.WriteString("opcache.jit_buffer_size = 100M\n")
	buf.WriteString("opcache.jit = tracing\n\n")

	buf.WriteString("; Realpath cache - speeds up file path resolution\n")
	buf.WriteString("realpath_cache_size = " + phpCfg.RealpathCacheSize + "\n")
	buf.WriteString("realpath_cache_ttl = 600\n\n")

	buf.WriteString("memory_limit = " + phpCfg.MemoryLimit + "\n")
	buf.WriteString("max_execution_time = " + fmt.Sprintf("%d", phpCfg.MaxExecutionTime) + "\n")
	buf.WriteString("max_input_time = " + fmt.Sprintf("%d", phpCfg.MaxInputTime) + "\n")
	buf.WriteString("max_input_vars = " + fmt.Sprintf("%d", phpCfg.MaxInputVars) + "\n")
	buf.WriteString("max_file_uploads = " + fmt.Sprintf("%d", phpCfg.MaxFileUploads) + "\n")
	buf.WriteString("post_max_size = " + phpCfg.PostMaxSize + "\n")
	buf.WriteString("upload_max_filesize = " + phpCfg.UploadMaxFilesize + "\n")
	buf.WriteString("default_charset = \"" + phpCfg.DefaultCharset + "\"\n\n")

	buf.WriteString("; Session\n")
	buf.WriteString("session.gc_maxlifetime = " + fmt.Sprintf("%d", phpCfg.SessionGCMaxLifetime) + "\n")
	buf.WriteString("session.cookie_lifetime = " + fmt.Sprintf("%d", phpCfg.SessionGCMaxLifetime) + "\n\n")

	fmt.Fprintf(&buf, "extension_dir = \"%s\"\n\n", extDir)

	if usePrefix {
		buf.WriteString("extension = php_mysqli.dll\n")
		buf.WriteString("extension = php_mbstring.dll\n")
	} else {
		buf.WriteString("extension = mysqli\n")
		buf.WriteString("extension = mbstring\n")
	}
	buf.WriteString("\n")

	buf.WriteString("; FastCGI\n")
	buf.WriteString("cgi.fix_pathinfo = " + fixPathinfo + "\n")
	buf.WriteString("cgi.force_redirect = 0\n")
	buf.WriteString("fastcgi.impersonate = 1\n")
	buf.WriteString("fastcgi.logging = 1\n\n")

	buf.WriteString("date.timezone = " + phpCfg.Timezone + "\n")

	return buf.String()
}
