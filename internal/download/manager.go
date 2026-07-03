package download

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ReleaseInfo struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	MirrorURL   string `json:"mirrorUrl,omitempty"`
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
		client:  &http.Client{Timeout: 30 * time.Minute},
		dataDir: dataDir,
	}
}

func (m *Manager) tryDownload(urls []string, destPath string, callback ProgressCallback) error {
	var lastErr error
	for _, u := range urls {
		if u == "" {
			continue
		}
		err := m.download(u, destPath, callback)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("所有下载源均失败: %w", lastErr)
}

func (m *Manager) download(u, destPath string, callback ProgressCallback) error {
	resp, err := m.client.Get(u)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("文件不存在 (404)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务器返回状态码: %d", resp.StatusCode)
	}

	return m.writeResponse(resp, destPath, callback)
}

func (m *Manager) writeResponse(resp *http.Response, destPath string, callback ProgressCallback) error {
	os.MkdirAll(filepath.Dir(destPath), 0755)

	out, err := os.Create(destPath + ".tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer out.Close()

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	lastCallback := time.Now()

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
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
			out.Close()
			return fmt.Errorf("读取响应失败: %w", err)
		}
	}

	if callback != nil {
		callback(total, total)
	}

	out.Close()
	return os.Rename(destPath+".tmp", destPath)
}

func (m *Manager) ExtractZip(src, dest string) error {
	reader, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("打开压缩文件失败: %w", err)
	}
	defer reader.Close()

	entries := reader.File
	if len(entries) == 0 {
		return nil
	}

	topPrefix := detectSingleRootDir(entries)
	if topPrefix != "" {
		for _, f := range entries {
			relative, _ := filepath.Rel(topPrefix, f.Name)
			if relative == "" || relative == "." {
				continue
			}
			fpath := filepath.Join(dest, relative)
			if f.FileInfo().IsDir() {
				os.MkdirAll(fpath, 0755)
				continue
			}
			os.MkdirAll(filepath.Dir(fpath), 0755)

			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("读取压缩条目失败 %s: %w", f.Name, err)
			}

			outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				rc.Close()
				return fmt.Errorf("创建文件失败 %s: %w", fpath, err)
			}

			_, err = io.Copy(outFile, rc)
			outFile.Close()
			rc.Close()
			if err != nil {
				return fmt.Errorf("解压文件失败 %s: %w", fpath, err)
			}
		}
		return nil
	}

	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}

	for _, f := range entries {
		fpath := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, 0755)
			continue
		}

		os.MkdirAll(filepath.Dir(fpath), 0755)

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("读取压缩条目失败 %s: %w", f.Name, err)
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return fmt.Errorf("创建文件失败 %s: %w", fpath, err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return fmt.Errorf("解压文件失败 %s: %w", fpath, err)
		}
	}
	return nil
}

func detectSingleRootDir(entries []*zip.File) string {
	roots := map[string]bool{}
	for _, f := range entries {
		name := filepath.ToSlash(f.Name)
		idx := strings.IndexByte(name, '/')
		if idx < 0 {
			return ""
		}
		roots[name[:idx]] = true
	}
	if len(roots) == 1 {
		for d := range roots {
			return d
		}
	}
	return ""
}

func (m *Manager) DownloadAndExtract(url, destDir string, callback ProgressCallback) error {
	return m.TryDownloadAndExtract([]string{url}, destDir, callback)
}

func (m *Manager) TryDownloadAndExtract(urls []string, destDir string, callback ProgressCallback) error {
	zipPath := filepath.Join(destDir, "package.zip")
	if err := m.tryDownload(urls, zipPath, callback); err != nil {
		return err
	}
	defer os.Remove(zipPath)
	return m.ExtractZip(zipPath, destDir)
}

func (m *Manager) GetAvailableVersions(serviceType string) ([]ReleaseInfo, error) {
	return m.getFallbackVersions(serviceType), nil
}

func (m *Manager) getFallbackVersions(serviceType string) []ReleaseInfo {
	switch serviceType {
	case "nginx":
		return []ReleaseInfo{
			{Version: "1.22.1", URL: "https://nginx.org/download/nginx-1.22.1.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/nginx/nginx-1.22.1.zip", FileName: "nginx-1.22.1.zip"},
			{Version: "1.24.0", URL: "https://nginx.org/download/nginx-1.24.0.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/nginx/nginx-1.24.0.zip", FileName: "nginx-1.24.0.zip"},
			{Version: "1.26.0", URL: "https://nginx.org/download/nginx-1.26.0.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/nginx/nginx-1.26.0.zip", FileName: "nginx-1.26.0.zip"},
		}
	case "mysql":
		return []ReleaseInfo{
			{Version: "5.6.51", URL: "https://dev.mysql.com/get/Downloads/MySQL-5.6/mysql-5.6.51-winx64.zip", MirrorURL: "https://mirrors.aliyun.com/mysql/MySQL-5.6/mysql-5.6.51-winx64.zip", FileName: "mysql-5.6.51-winx64.zip"},
			{Version: "5.7.44", URL: "https://dev.mysql.com/get/Downloads/MySQL-5.7/mysql-5.7.44-winx64.zip", MirrorURL: "https://mirrors.aliyun.com/mysql/MySQL-5.7/mysql-5.7.44-winx64.zip", FileName: "mysql-5.7.44-winx64.zip"},
			{Version: "8.0.36", URL: "https://dev.mysql.com/get/Downloads/MySQL-8.0/mysql-8.0.36-winx64.zip", MirrorURL: "https://mirrors.aliyun.com/mysql/MySQL-8.0/mysql-8.0.36-winx64.zip", FileName: "mysql-8.0.36-winx64.zip"},
			{Version: "8.4.0", URL: "https://dev.mysql.com/get/Downloads/MySQL-8.4/mysql-8.4.0-winx64.zip", MirrorURL: "https://mirrors.aliyun.com/mysql/MySQL-8.4/mysql-8.4.0-winx64.zip", FileName: "mysql-8.4.0-winx64.zip"},
		}
	case "redis":
		return []ReleaseInfo{
			{Version: "5.0.14", URL: "https://github.com/redis-windows/redis-windows/releases/download/5.0.14/Redis-5.0.14-Windows-x64.zip", FileName: "Redis-5.0.14-Windows-x64.zip"},
			{Version: "6.0.20", URL: "https://github.com/redis-windows/redis-windows/releases/download/6.0.20/Redis-6.0.20-Windows-x64.zip", FileName: "Redis-6.0.20-Windows-x64.zip"},
			{Version: "7.2.0", URL: "https://github.com/redis-windows/redis-windows/releases/download/7.2.0/Redis-7.2.0-Windows-x64.zip", FileName: "Redis-7.2.0-Windows-x64.zip"},
			{Version: "7.4.0", URL: "https://github.com/redis-windows/redis-windows/releases/download/7.4.0/Redis-7.4.0-Windows-x64.zip", FileName: "Redis-7.4.0-Windows-x64.zip"},
		}
	case "php":
		return []ReleaseInfo{
			{Version: "5.5.38", URL: "https://downloads.php.net/~windows/releases/archives/php-5.5.38-nts-Win32-VC11-x64.zip", FileName: "php-5.5.38.zip"},
			{Version: "5.6.40", URL: "https://downloads.php.net/~windows/releases/archives/php-5.6.40-nts-Win32-VC11-x64.zip", FileName: "php-5.6.40.zip"},
			{Version: "7.0.33", URL: "https://downloads.php.net/~windows/releases/archives/php-7.0.33-nts-Win32-VC14-x64.zip", FileName: "php-7.0.33.zip"},
			{Version: "7.1.33", URL: "https://downloads.php.net/~windows/releases/archives/php-7.1.33-nts-Win32-VC14-x64.zip", FileName: "php-7.1.33.zip"},
			{Version: "7.2.34", URL: "https://downloads.php.net/~windows/releases/archives/php-7.2.34-nts-Win32-VC15-x64.zip", FileName: "php-7.2.34.zip"},
			{Version: "7.3.33", URL: "https://downloads.php.net/~windows/releases/archives/php-7.3.33-nts-Win32-VC15-x64.zip", FileName: "php-7.3.33.zip"},
			{Version: "7.4.33", URL: "https://downloads.php.net/~windows/releases/php-7.4.33-nts-Win32-vc15-x64.zip", MirrorURL: "https://mirrors.aliyun.com/php/php-7.4.33-nts-Win32-vc15-x64.zip", FileName: "php-7.4.33.zip"},
			{Version: "8.0.30", URL: "https://downloads.php.net/~windows/releases/php-8.0.30-nts-Win32-vs16-x64.zip", MirrorURL: "https://mirrors.aliyun.com/php/php-8.0.30-nts-Win32-vs16-x64.zip", FileName: "php-8.0.30.zip"},
			{Version: "8.1.34", URL: "https://downloads.php.net/~windows/releases/php-8.1.34-nts-Win32-vs16-x64.zip", MirrorURL: "https://mirrors.aliyun.com/php/php-8.1.34-nts-Win32-vs16-x64.zip", FileName: "php-8.1.34.zip"},
			{Version: "8.2.32", URL: "https://downloads.php.net/~windows/releases/php-8.2.32-nts-Win32-vs16-x64.zip", MirrorURL: "https://mirrors.aliyun.com/php/php-8.2.32-nts-Win32-vs16-x64.zip", FileName: "php-8.2.32.zip"},
			{Version: "8.3.32", URL: "https://downloads.php.net/~windows/releases/php-8.3.32-nts-Win32-vs16-x64.zip", MirrorURL: "https://mirrors.aliyun.com/php/php-8.3.32-nts-Win32-vs16-x64.zip", FileName: "php-8.3.32.zip"},
			{Version: "8.4.23", URL: "https://downloads.php.net/~windows/releases/php-8.4.23-nts-Win32-vs17-x64.zip", MirrorURL: "https://mirrors.aliyun.com/php/php-8.4.23-nts-Win32-vs17-x64.zip", FileName: "php-8.4.23.zip"},
			{Version: "8.5.8", URL: "https://downloads.php.net/~windows/releases/php-8.5.8-nts-Win32-vs17-x64.zip", MirrorURL: "https://mirrors.aliyun.com/php/php-8.5.8-nts-Win32-vs17-x64.zip", FileName: "php-8.5.8.zip"},
		}
	case "node":
		return []ReleaseInfo{
			{Version: "16.20.2", URL: "https://nodejs.org/dist/v16.20.2/node-v16.20.2-win-x64.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/v16.20.2/node-v16.20.2-win-x64.zip", FileName: "node-v16.20.2-win-x64.zip"},
			{Version: "18.20.4", URL: "https://nodejs.org/dist/v18.20.4/node-v18.20.4-win-x64.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/v18.20.4/node-v18.20.4-win-x64.zip", FileName: "node-v18.20.4-win-x64.zip"},
			{Version: "20.18.0", URL: "https://nodejs.org/dist/v20.18.0/node-v20.18.0-win-x64.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/v20.18.0/node-v20.18.0-win-x64.zip", FileName: "node-v20.18.0-win-x64.zip"},
			{Version: "22.8.0", URL: "https://nodejs.org/dist/v22.8.0/node-v22.8.0-win-x64.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/v22.8.0/node-v22.8.0-win-x64.zip", FileName: "node-v22.8.0-win-x64.zip"},
		}
	case "python":
		return []ReleaseInfo{
			{Version: "3.9.13", URL: "https://www.python.org/ftp/python/3.9.13/python-3.9.13-embed-amd64.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/python/3.9.13/python-3.9.13-embed-amd64.zip", FileName: "python-3.9.13-embed-amd64.zip"},
			{Version: "3.10.11", URL: "https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/python/3.10.11/python-3.10.11-embed-amd64.zip", FileName: "python-3.10.11-embed-amd64.zip"},
			{Version: "3.11.11", URL: "https://www.python.org/ftp/python/3.11.11/python-3.11.11-embed-amd64.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/python/3.11.11/python-3.11.11-embed-amd64.zip", FileName: "python-3.11.11-embed-amd64.zip"},
			{Version: "3.12.8", URL: "https://www.python.org/ftp/python/3.12.8/python-3.12.8-embed-amd64.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/python/3.12.8/python-3.12.8-embed-amd64.zip", FileName: "python-3.12.8-embed-amd64.zip"},
			{Version: "3.13.2", URL: "https://www.python.org/ftp/python/3.13.2/python-3.13.2-embed-amd64.zip", MirrorURL: "https://mirrors.tuna.tsinghua.edu.cn/python/3.13.2/python-3.13.2-embed-amd64.zip", FileName: "python-3.13.2-embed-amd64.zip"},
		}
	default:
		return nil
	}
}
