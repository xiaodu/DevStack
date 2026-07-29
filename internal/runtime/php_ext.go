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
		re := regexp.MustCompile(`(?m)^\s*extension\s*=\s*(\S+)`)
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

func saveExtensionList(cfg *config.Config, version string, extensionNames []string) error {
	phpDir := filepath.Join(cfg.Get().InstallDir, "programs", "php", version)
	extDir := filepath.Join(phpDir, "ext")
	phpIni := filepath.Join(phpDir, "php.ini")

	if !fileExists(phpIni) {
		ini := GeneratePHPIniContent(cfg, phpDir, version)
		os.WriteFile(phpIni, []byte(ini), 0644)
	}

	builtExtensions := make(map[string]string)
	if entries, err := os.ReadDir(extDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			fname := entry.Name()
			if strings.HasSuffix(fname, ".dll") || strings.HasSuffix(fname, ".so") {
				extName := cleanExtName(fname)
				builtExtensions[extName] = fname
			}
		}
	}

	for _, name := range extensionNames {
		if _, ok := builtExtensions[name]; !ok {
			withPrefix := "php_" + name
			if _, ok2 := builtExtensions[withPrefix]; ok2 {
				continue
			}
			return fmt.Errorf("扩展 %s 未在 ext/ 目录中找到对应的 .dll 文件", name)
		}
	}

	data, err := os.ReadFile(phpIni)
	if err != nil {
		return err
	}
	content := string(data)

	extRe := regexp.MustCompile(`(?m)^[ \t]*;?[ \t]*extension[ \t]*=[ \t]*\S+.*$`)
	content = extRe.ReplaceAllString(content, "")

	extDirRe := regexp.MustCompile(`(?m)^[ \t]*;?\s*extension_dir\s*=.*$`)
	content = extDirRe.ReplaceAllString(content, "")
	extDirLine := fmt.Sprintf(`extension_dir = "%s"`, filepath.ToSlash(extDir))

	usePrefix := !phpVersionAtLeast(version, 7, 2)
	content = strings.TrimSpace(content)
	content += "\n" + extDirLine + "\n"
	for _, name := range extensionNames {
		extLine := name
		if usePrefix {
			if f, ok := builtExtensions[name]; ok {
				extLine = f
			}
		}
		content += "extension = " + extLine + "\n"
	}

	if err := os.WriteFile(phpIni, []byte(content), 0644); err != nil {
		return err
	}

	written, err := os.ReadFile(phpIni)
	if err != nil {
		return err
	}
	writtenStr := string(written)
	for _, name := range extensionNames {
		checkName := name
		if usePrefix {
			if f, ok := builtExtensions[name]; ok {
				checkName = f
			}
		}
		if !strings.Contains(writtenStr, "extension = "+checkName) {
			return fmt.Errorf("写入验证失败：php.ini 中未找到 extension = %s", checkName)
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

	buf.WriteString("error_reporting = E_ALL & ~E_NOTICE & ~E_DEPRECATED & ~E_STRICT\n")
	buf.WriteString("display_errors = " + displayErrors + "\n")
	buf.WriteString("display_startup_errors = " + displayErrors + "\n")
	buf.WriteString("log_errors = On\n")
	buf.WriteString("error_log = \"php_errors.log\"\n\n")

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
	buf.WriteString("max_input_time = " + fmt.Sprintf("%d", phpCfg.MaxExecutionTime) + "\n")
	buf.WriteString("max_input_vars = 3000\n")
	buf.WriteString("post_max_size = " + phpCfg.PostMaxSize + "\n")
	buf.WriteString("upload_max_filesize = " + phpCfg.UploadMaxFilesize + "\n\n")

	buf.WriteString("; Session\n")
	buf.WriteString("session.gc_maxlifetime = 7200\n")
	buf.WriteString("session.cookie_lifetime = 7200\n\n")

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
	buf.WriteString("cgi.fix_pathinfo = 1\n")
	buf.WriteString("cgi.force_redirect = 0\n")
	buf.WriteString("fastcgi.impersonate = 1\n")
	buf.WriteString("fastcgi.logging = 1\n\n")

	buf.WriteString("date.timezone = Asia/Shanghai\n")

	return buf.String()
}
