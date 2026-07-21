package project

import (
	"fmt"
	"path/filepath"

	"github.com/xiaodu/DevStack/internal/core"
)

func (m *Manager) buildConfig(p core.Project) string {
	root := p.Root
	if root == "" {
		root = filepath.Join(m.installDir, "www")
	}

	listenPort := p.Port
	if listenPort == 0 {
		listenPort = 80
	}

	conf := fmt.Sprintf("server {\n    listen %d;\n", listenPort)

	if p.SSL {
		httpsPort := p.HTTPSPort
		if httpsPort == 0 {
			httpsPort = 443
		}
		conf += fmt.Sprintf("    listen %d ssl;\n", httpsPort)
	}

	conf += fmt.Sprintf(`    server_name %s;
    root %s;
`, p.Domain, filepath.ToSlash(root))

	switch p.Type {
	case core.ProjectPHP:
		conf += m.buildPHPConfig(p)
	case core.ProjectNode:
		conf += m.buildNodeConfig(p)
	case core.ProjectPython:
		conf += m.buildPythonConfig(p)
	case core.ProjectProxy:
		conf += m.buildProxyConfig(p)
	default:
		conf += m.buildStaticConfig(p)
	}

	if p.SSL {
		conf += `
    ssl_certificate certs/server.crt;
    ssl_certificate_key certs/server.key;
`
	}

	conf += "}\n"
	return conf
}

func (m *Manager) buildPHPConfig(p core.Project) string {
	conf := "    index index.html index.htm index.php;\n\n"

	switch p.Rewrite {
	case "laravel":
		conf += `    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }
`
	case "thinkphp":
		conf += `    location / {
        try_files $uri $uri/ /index.php?s=$uri;
    }
`
	default:
		conf += `    location / {
        try_files $uri $uri/ =404;
    }
`
	}

	phpPort := 9000
	if m.runtime != nil {
		if port := m.runtime.GetPHPVersionPort(p.PHPVersion); port > 0 {
			phpPort = port
		}
	}

	conf += fmt.Sprintf(`
    location ~ \.php$ {
        fastcgi_pass 127.0.0.1:%d;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }
`, phpPort)

	return conf
}

func (m *Manager) buildNodeConfig(p core.Project) string {
	targetPort := p.TargetPort
	if targetPort == 0 {
		targetPort = 3000
	}
	return fmt.Sprintf(`    index index.html index.htm;
    location / {
        proxy_pass http://127.0.0.1:%d;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
`, targetPort)
}

func (m *Manager) buildPythonConfig(p core.Project) string {
	targetPort := p.TargetPort
	if targetPort == 0 {
		targetPort = 5000
	}
	return fmt.Sprintf(`    index index.html index.htm;
    location / {
        proxy_pass http://127.0.0.1:%d;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
`, targetPort)
}

func (m *Manager) buildProxyConfig(p core.Project) string {
	target := p.ProxyPass
	if target == "" {
		if p.TargetPort > 0 {
			target = fmt.Sprintf("http://127.0.0.1:%d", p.TargetPort)
		} else {
			target = "http://127.0.0.1:3000"
		}
	}
	return fmt.Sprintf(`    index index.html index.htm;
    location / {
        proxy_pass %s;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
`, target)
}

func (m *Manager) buildStaticConfig(p core.Project) string {
	return `    index index.html index.htm;
    location / {
        try_files $uri $uri/ =404;
    }
`
}
