package site

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/xiaodu/DevStack/internal/core"
)

type Manager struct {
	mu        sync.RWMutex
	sites     map[string]*core.Site
	filePath  string
	installDir string
	nginxVer  string
	phpPort   int
	phpPorts  map[string]int
}

func NewManager(installDir, nginxVer string, phpPort int) *Manager {
	m := &Manager{
		sites:      make(map[string]*core.Site),
		filePath:   filepath.Join(installDir, "data", "sites.json"),
		installDir: installDir,
		nginxVer:   nginxVer,
		phpPort:    phpPort,
		phpPorts:   make(map[string]int),
	}
	m.load()
	return m
}

func (m *Manager) List() []core.Site {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]core.Site, 0, len(m.sites))
	for _, s := range m.sites {
		result = append(result, *s)
	}
	return result
}

func (m *Manager) Get(id string) (core.Site, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sites[id]
	if !ok {
		return core.Site{}, fmt.Errorf("站点 %s 未找到", id)
	}
	return *s, nil
}

func (m *Manager) Create(s core.Site) (*core.Site, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s.ID == "" {
		s.ID = fmt.Sprintf("site_%d", time.Now().UnixMilli())
	}
	s.CreatedAt = time.Now().Format("2006-01-02 15:04:05")

	m.sites[s.ID] = &s
	if err := m.save(); err != nil {
		delete(m.sites, s.ID)
		return nil, err
	}
	m.syncNginx()
	if s.Enabled {
		if err := ensureHostsEntry(s.Domain, true); err != nil {
			return &s, err
		}
	}
	return &s, nil
}

func (m *Manager) Update(s core.Site) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old, ok := m.sites[s.ID]
	if !ok {
		return fmt.Errorf("站点 %s 未找到", s.ID)
	}
	if old.Domain != s.Domain {
		ensureHostsEntry(old.Domain, false)
		if s.Enabled {
			ensureHostsEntry(s.Domain, true)
		}
	}
	m.sites[s.ID] = &s
	if err := m.save(); err != nil {
		return err
	}
	m.syncNginx()
	return nil
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sites[id]
	if !ok {
		return fmt.Errorf("站点 %s 未找到", id)
	}
	ensureHostsEntry(s.Domain, false)
	delete(m.sites, id)
	if err := m.save(); err != nil {
		return err
	}
	m.syncNginx()
	return nil
}

func (m *Manager) Toggle(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sites[id]
	if !ok {
		return fmt.Errorf("站点 %s 未找到", id)
	}
	s.Enabled = !s.Enabled
	if s.Enabled {
		ensureHostsEntry(s.Domain, true)
	} else {
		ensureHostsEntry(s.Domain, false)
	}
	if err := m.save(); err != nil {
		return err
	}
	m.syncNginx()
	return nil
}

func (m *Manager) nginxDir() string {
	if m.installDir == "" || m.nginxVer == "" {
		return ""
	}
	return filepath.Join(m.installDir, "nginx", m.nginxVer)
}

func (m *Manager) syncNginx() {
	nd := m.nginxDir()
	if nd == "" {
		return
	}
	sitesDir := filepath.Join(nd, "conf", "sites-enabled")
	os.MkdirAll(sitesDir, 0755)
	os.MkdirAll(filepath.Join(m.installDir, "www"), 0755)

	entries, _ := os.ReadDir(sitesDir)
	for _, e := range entries {
		os.Remove(filepath.Join(sitesDir, e.Name()))
	}

	for _, s := range m.sites {
		if !s.Enabled {
			continue
		}
		conf := m.buildVhostConfig(*s)
		confPath := filepath.Join(sitesDir, s.ID+".conf")
		os.WriteFile(confPath, []byte(conf), 0644)
	}

	m.ensureNginxInclude()
	m.reloadNginx()
}

func (m *Manager) buildVhostConfig(s core.Site) string {
	root := s.DocumentRoot
	if root == "" {
		root = filepath.Join(m.installDir, "www")
	}

	useSSL := s.SSL
	if s.Protocol == "https" || s.Protocol == "both" {
		useSSL = true
	}

	listenPort := s.Port
	if listenPort == 0 {
		listenPort = 80
	}

	conf := fmt.Sprintf("server {\n    listen %d", listenPort)
	if useSSL && s.Protocol != "https" {
		conf += ";\n"
	}

	if s.Protocol == "https" || s.Protocol == "both" {
		httpsPort := s.HTTPSPort
		if httpsPort == 0 {
			httpsPort = 443
		}
		conf += fmt.Sprintf("\n    listen %d ssl", httpsPort)
	}

	conf += fmt.Sprintf(`;
    server_name %s;
    root %s;
`, s.Domain, filepath.ToSlash(root))

	isPHP := s.Type == "php" || s.PHPEnabled || s.PHPVersion != ""

	if isPHP {
		conf += "    index index.html index.htm index.php;\n"
	} else {
		conf += "    index index.html index.htm;\n"
	}

	conf += "\n"

	rewriteRule := s.Rewrite
	if rewriteRule == "" {
		rewriteRule = "default"
	}

	if s.Type == "proxy" {
		targetPort := listenPort + 1000
		conf += `    location / {
        proxy_pass http://127.0.0.1:` + fmt.Sprintf("%d", targetPort) + `;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
`
	} else if rewriteRule == "laravel" {
		conf += `    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }
`
	} else if rewriteRule == "thinkphp" {
		conf += `    location / {
        try_files $uri $uri/ /index.php?s=$uri;
    }
`
	} else {
		conf += `    location / {
        try_files $uri $uri/ =404;
    }
`
	}

	if isPHP {
		phpPort := m.GetPHPVersionPort(s.PHPVersion)
		if phpPort == 0 {
			phpPort = m.phpPort
		}
		if phpPort == 0 {
			phpPort = 9000
		}
		conf += fmt.Sprintf(`
    location ~ \.php$ {
        fastcgi_pass 127.0.0.1:%d;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }
`, phpPort)
	}

	if useSSL {
		conf += `
    ssl_certificate certs/server.crt;
    ssl_certificate_key certs/server.key;
`
	}

	conf += "}\n"
	return conf
}

func (m *Manager) ensureNginxInclude() {
	nd := m.nginxDir()
	if nd == "" {
		return
	}
	nginxConf := filepath.Join(nd, "conf", "nginx.conf")
	data, err := os.ReadFile(nginxConf)
	if err != nil {
		return
	}
	includeLine := "include sites-enabled/*.conf;"
	if !containsString(string(data), includeLine) {
		patch := "\nhttp {\n    " + includeLine + "\n}\n"
		f, _ := os.OpenFile(nginxConf, os.O_APPEND|os.O_WRONLY, 0644)
		if f != nil {
			f.WriteString(patch)
			f.Close()
		}
	}
}

func (m *Manager) reloadNginx() {
	nd := m.nginxDir()
	if nd == "" {
		return
	}
	nginxExe := filepath.Join(nd, "nginx.exe")
	if _, err := os.Stat(nginxExe); err != nil {
		return
	}
	cmd := exec.Command(nginxExe, "-s", "reload")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Run()
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && containsFunc(s, substr)
}

func containsFunc(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (m *Manager) load() {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return
	}
	var sites []core.Site
	if json.Unmarshal(data, &sites) != nil {
		return
	}
	for i := range sites {
		m.sites[sites[i].ID] = &sites[i]
	}
}

func (m *Manager) save() error {
	sites := make([]core.Site, 0, len(m.sites))
	for _, s := range m.sites {
		sites = append(sites, *s)
	}
	data, err := json.MarshalIndent(sites, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(m.filePath), 0755)
	return os.WriteFile(m.filePath, data, 0644)
}

func (m *Manager) SetPHPVersionPort(version string, port int) {
	m.phpPorts[version] = port
}

func (m *Manager) GetPHPVersionPort(version string) int {
	if port, ok := m.phpPorts[version]; ok {
		return port
	}
	return 9000
}
