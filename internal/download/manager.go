package download

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type ReleaseInfo struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	FileName    string `json:"fileName"`
	Size        int64  `json:"size"`
	Checksum    string `json:"checksum,omitempty"`
	ReleaseDate string `json:"releaseDate"`
}

type ProgressCallback func(downloaded, total int64)

type Manager struct {
	client  *http.Client
	dataDir string
}

func NewManager(dataDir string) *Manager {
	return &Manager{
		client: &http.Client{Timeout: 30 * time.Minute},
		dataDir: dataDir,
	}
}

func (m *Manager) Download(url, destPath string, callback ProgressCallback) error {
	os.MkdirAll(filepath.Dir(destPath), 0755)

	out, err := os.Create(destPath + ".tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer out.Close()

	resp, err := m.client.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，状态码: %d", resp.StatusCode)
	}

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	lastCallback := time.Now()

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return fmt.Errorf("写入文件失败: %w", werr)
			}
			downloaded += int64(n)
			if callback != nil && time.Since(lastCallback) > 200*time.Millisecond {
				callback(downloaded, total)
				lastCallback = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取响应失败: %w", err)
		}
	}

	if callback != nil {
		callback(total, total)
	}

	os.Rename(destPath+".tmp", destPath)
	return nil
}

func (m *Manager) GetAvailableVersions(serviceType string) ([]ReleaseInfo, error) {
	manifestURL := fmt.Sprintf("https://servbay-manager.example.com/manifest/%s.json", serviceType)
	resp, err := m.client.Get(manifestURL)
	if err != nil {
		return m.getFallbackVersions(serviceType), nil
	}
	defer resp.Body.Close()

	var releases []ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return m.getFallbackVersions(serviceType), nil
	}
	return releases, nil
}

func (m *Manager) getFallbackVersions(serviceType string) []ReleaseInfo {
	switch serviceType {
	case "nginx":
		return []ReleaseInfo{
			{Version: "1.24.0", URL: "https://nginx.org/download/nginx-1.24.0.zip", FileName: "nginx-1.24.0.zip", Size: 0},
			{Version: "1.26.0", URL: "https://nginx.org/download/nginx-1.26.0.zip", FileName: "nginx-1.26.0.zip", Size: 0},
		}
	case "mysql":
		return []ReleaseInfo{
			{Version: "8.0.36", URL: "https://dev.mysql.com/get/Downloads/MySQL-8.0/mysql-8.0.36-winx64.zip", FileName: "mysql-8.0.36-winx64.zip", Size: 0},
			{Version: "8.4.0", URL: "https://dev.mysql.com/get/Downloads/MySQL-8.4/mysql-8.4.0-winx64.zip", FileName: "mysql-8.4.0-winx64.zip", Size: 0},
		}
	case "php":
		return []ReleaseInfo{
			{Version: "8.2.20", URL: "https://windows.php.net/downloads/releases/php-8.2.20-nts-Win32-vs16-x64.zip", FileName: "php-8.2.20.zip", Size: 0},
			{Version: "8.3.8", URL: "https://windows.php.net/downloads/releases/php-8.3.8-nts-Win32-vs16-x64.zip", FileName: "php-8.3.8.zip", Size: 0},
			{Version: "8.4.0", URL: "https://windows.php.net/downloads/releases/php-8.4.0-nts-Win32-vs16-x64.zip", FileName: "php-8.4.0.zip", Size: 0},
		}
	case "node":
		return []ReleaseInfo{
			{Version: "18.19.0", URL: "https://nodejs.org/dist/v18.19.0/node-v18.19.0-win-x64.zip", FileName: "node-v18.19.0-win-x64.zip", Size: 0},
			{Version: "20.11.0", URL: "https://nodejs.org/dist/v20.11.0/node-v20.11.0-win-x64.zip", FileName: "node-v20.11.0-win-x64.zip", Size: 0},
			{Version: "22.0.0", URL: "https://nodejs.org/dist/v22.0.0/node-v22.0.0-win-x64.zip", FileName: "node-v22.0.0-win-x64.zip", Size: 0},
		}
	case "python":
		return []ReleaseInfo{
			{Version: "3.11.8", URL: "https://www.python.org/ftp/python/3.11.8/python-3.11.8-embed-amd64.zip", FileName: "python-3.11.8-embed-amd64.zip", Size: 0},
			{Version: "3.12.0", URL: "https://www.python.org/ftp/python/3.12.0/python-3.12.0-embed-amd64.zip", FileName: "python-3.12.0-embed-amd64.zip", Size: 0},
		}
	default:
		return nil
	}
}
