package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/xiaodu/DevStack/internal/config"
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type PHPExtensionInfo struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type PHPHandler struct {
	BaseHandler
}

type PHPVersionInfo struct {
	Version string `json:"version"`
	Port    int    `json:"port"`
}

type PHPRegistry struct {
	Versions []PHPVersionInfo `json:"versions"`
}

func getRegistryPath(cfg *config.Config) string {
	return filepath.Join(cfg.Get().DataDir, "php_registry.json")
}

func LoadPHPRegistry(cfg *config.Config) *PHPRegistry {
	reg := &PHPRegistry{Versions: make([]PHPVersionInfo, 0)}
	data, err := os.ReadFile(getRegistryPath(cfg))
	if err != nil {
		return reg
	}
	json.Unmarshal(data, reg)
	return reg
}

func SavePHPRegistry(cfg *config.Config, reg *PHPRegistry) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(getRegistryPath(cfg)), 0755)
	return os.WriteFile(getRegistryPath(cfg), data, 0644)
}

func GetPHPVersionPort(cfg *config.Config, version string) int {
	reg := LoadPHPRegistry(cfg)
	for _, v := range reg.Versions {
		if v.Version == version {
			return v.Port
		}
	}
	return 0
}

func RegisterPHPVersion(cfg *config.Config, version string, port int) {
	reg := LoadPHPRegistry(cfg)
	for i, v := range reg.Versions {
		if v.Version == version {
			reg.Versions[i].Port = port
			SavePHPRegistry(cfg, reg)
			return
		}
	}
	reg.Versions = append(reg.Versions, PHPVersionInfo{Version: version, Port: port})
	SavePHPRegistry(cfg, reg)
}

func AutoAssignPorts(cfg *config.Config, installDir string) {
	phpDir := filepath.Join(installDir, "php")
	entries, err := os.ReadDir(phpDir)
	if err != nil {
		return
	}

	reg := LoadPHPRegistry(cfg)
	existingPorts := make(map[int]bool)
	for _, v := range reg.Versions {
		if v.Port > 0 {
			existingPorts[v.Port] = true
		}
	}

	basePort := 9001
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ver := entry.Name()
		found := false
		for _, v := range reg.Versions {
			if v.Version == ver {
				found = true
				break
			}
		}
		if !found {
			for existingPorts[basePort] {
				basePort++
			}
			reg.Versions = append(reg.Versions, PHPVersionInfo{Version: ver, Port: basePort})
			existingPorts[basePort] = true
			basePort++
		}
	}
	SavePHPRegistry(cfg, reg)
}

func (h *PHPHandler) Configure(s *ServiceInstance, cfg *config.Config) error {
	installDir := cfg.Get().InstallDir
	phpDir := filepath.Join(installDir, "php", s.Info.Version)

	phpIni := filepath.Join(phpDir, "php.ini")
	if fileExists(phpIni) {
		return nil
	}

	prod := filepath.Join(phpDir, "php.ini-production")
	dev := filepath.Join(phpDir, "php.ini-development")
	if fileExists(prod) {
		data, err := os.ReadFile(prod)
		if err != nil {
			return err
		}
		return os.WriteFile(phpIni, data, 0644)
	}
	if fileExists(dev) {
		data, err := os.ReadFile(dev)
		if err != nil {
			return err
		}
		return os.WriteFile(phpIni, data, 0644)
	}

	ini := generatePHPIniContent(phpDir, s.Info.Version)
	return os.WriteFile(phpIni, []byte(ini), 0644)
}

func (h *PHPHandler) Start(s *ServiceInstance, cfg *config.Config, pm *process.Manager) error {
	installDir := cfg.Get().InstallDir
	phpDir := filepath.Join(installDir, "php", s.Info.Version)

	phpExe := filepath.Join(phpDir, "php-cgi.exe")
	if !fileExists(phpExe) {
		if fileExists(filepath.Join(phpDir, "php.exe")) {
			s.Info.Status = core.StatusError
			s.Info.Error = "未找到 php-cgi.exe，当前只有 php.exe（CLI 模式），无法作为 FastCGI 启动"
			return fmt.Errorf("php-cgi.exe 未找到，只有 php.exe (CLI)")
		}
		return fmt.Errorf("PHP 可执行文件未找到: %s", phpExe)
	}

	phpIni := filepath.Join(phpDir, "php.ini")
	if !fileExists(phpIni) {
		ini := generatePHPIniContent(phpDir, s.Info.Version)
		os.WriteFile(phpIni, []byte(ini), 0644)
	}

	s.Info.Status = core.StatusStarting
	phpEnv := []string{"PHP_FCGI_MAX_REQUESTS=10000"}
	var pids []int

	for i := 0; i < phpWorkerCount; i++ {
		port := s.Info.Port + i
		args := []string{
			"-b", fmt.Sprintf("127.0.0.1:%d", port),
			"-c", phpIni,
		}
		pid, err := pm.Start("php", phpExe, args, phpEnv, phpDir)
		if err != nil {
			for _, p := range pids {
				pm.Stop(p)
			}
			s.Info.Status = core.StatusError
			s.Info.Error = err.Error()
			return err
		}
		pids = append(pids, pid)
	}

	for i := 0; i < 6; i++ {
		allRunning := true
		for _, pid := range pids {
			if !pm.IsRunning(pid) {
				allRunning = false
				break
			}
		}
		if allRunning {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	for _, pid := range pids {
		if !pm.IsRunning(pid) {
			for _, p := range pids {
				pm.Stop(p)
			}
			s.Info.Status = core.StatusError
			s.Info.Error = "PHP 进程启动后立即退出，请检查 VC 运行库是否完整"
			return fmt.Errorf("PHP 进程已退出，可能缺少 VC 运行库或配置错误")
		}
	}

	s.process = &execWrapper{
		pid:       pids[0],
		extraPids: pids[1:],
		cmd:       phpExe,
		started:   time.Now(),
	}
	s.Info.Status = core.StatusRunning
	s.Info.PID = pids[0]
	return nil
}

func (h *PHPHandler) SaveConfig(s *ServiceInstance, cfg *config.Config, values map[string]string) error {
	installDir := cfg.Get().InstallDir
	phpIni := filepath.Join(installDir, "php", s.Info.Version, "php.ini")
	if !fileExists(phpIni) {
		return h.Configure(s, cfg)
	}
	data, err := os.ReadFile(phpIni)
	if err != nil {
		return err
	}
	content := string(data)
	for k, v := range values {
		content = updateIniValue(content, k, v)
	}
	return os.WriteFile(phpIni, []byte(content), 0644)
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

func generatePHPIniContent(phpDir, version string) string {
	extDir := filepath.ToSlash(filepath.Join(phpDir, "ext"))
	usePrefix := !phpVersionAtLeast(version, 7, 2)

	var buf strings.Builder
	buf.WriteString("[PHP]\n")
	buf.WriteString("engine = On\n\n")

	buf.WriteString("error_reporting = E_ALL & ~E_NOTICE & ~E_DEPRECATED & ~E_STRICT\n")
	buf.WriteString("display_errors = On\n")
	buf.WriteString("display_startup_errors = On\n")
	buf.WriteString("log_errors = On\n")
	buf.WriteString("error_log = \"php_errors.log\"\n\n")

	buf.WriteString("opcache.enable = 1\n")
	buf.WriteString("opcache.memory_consumption = 128\n")
	buf.WriteString("opcache.interned_strings_buffer = 8\n")
	buf.WriteString("opcache.max_accelerated_files = 10000\n")
	buf.WriteString("opcache.revalidate_freq = 2\n")
	buf.WriteString("opcache.fast_shutdown = 1\n\n")

	buf.WriteString("memory_limit = 256M\n")
	buf.WriteString("max_execution_time = 300\n")
	buf.WriteString("max_input_time = 300\n")
	buf.WriteString("max_input_vars = 3000\n")
	buf.WriteString("post_max_size = 64M\n")
	buf.WriteString("upload_max_filesize = 64M\n\n")

	fmt.Fprintf(&buf, "extension_dir = \"%s\"\n\n", extDir)

	if usePrefix {
		buf.WriteString("extension = php_mysqli.dll\n")
		buf.WriteString("extension = php_mbstring.dll\n")
	} else {
		buf.WriteString("extension = mysqli\n")
		buf.WriteString("extension = mbstring\n")
	}
	buf.WriteString("\n")

	buf.WriteString("cgi.fix_pathinfo = 1\n")
	buf.WriteString("cgi.force_redirect = 0\n")
	buf.WriteString("fastcgi.impersonate = 1\n")
	buf.WriteString("fastcgi.logging = 1\n\n")

	buf.WriteString("date.timezone = Asia/Shanghai\n")

	return buf.String()
}

func (h *PHPHandler) Stop(s *ServiceInstance, pm *process.Manager) error {
	if s.process == nil || s.process.pid <= 0 {
		return nil
	}
	s.Info.Status = core.StatusStopping

	pids := append([]int{s.process.pid}, s.process.extraPids...)
	for _, pid := range pids {
		pm.Stop(pid)
	}

	s.process = nil
	s.Info.Status = core.StatusStopped
	s.Info.PID = 0
	return nil
}

func (h *PHPHandler) GetExtensions(s *ServiceInstance, cfg *config.Config) ([]PHPExtensionInfo, error) {
	phpDir := filepath.Join(cfg.Get().InstallDir, "php", s.Info.Version)
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

func (h *PHPHandler) SaveExtensions(s *ServiceInstance, cfg *config.Config, extensionNames []string) error {
	phpDir := filepath.Join(cfg.Get().InstallDir, "php", s.Info.Version)
	extDir := filepath.Join(phpDir, "ext")
	phpIni := filepath.Join(phpDir, "php.ini")

	if !fileExists(phpIni) {
		if err := h.Configure(s, cfg); err != nil {
			return err
		}
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

	usePrefix := !phpVersionAtLeast(s.Info.Version, 7, 2)
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


