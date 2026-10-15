package download

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaodu/DevStack/internal/registry"
	"github.com/xiaodu/DevStack/internal/tx"
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
	client     *http.Client
	installDir string
	cancels    sync.Map // key(string) -> context.CancelFunc
	// inflight 防止两个并发下载写同一个 destDir。
	// key 是 destDir（绝对路径），value 是 *sync.Mutex。
	// 前端重复点击安装同版本、SetupWizard 与 Packages 同时装同一服务版本时，
	// 两个调用的 key 可能不同（"download:mysql:8.4.3" vs "setup-download:mysql:8.4.3"），
	// 但 destDir 相同，锁绑定在 destDir 上能覆盖所有并发场景。
	inflight sync.Map
}

// perDirLock 返回 destDir 对应的互斥锁。首次访问时创建。
func (m *Manager) perDirLock(destDir string) *sync.Mutex {
	actual, _ := m.inflight.LoadOrStore(destDir, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// isInstallationComplete 判断 destDir 是否已经有完整安装内容。
// 锁拿到后、开始下载前调用，若已有安装则第二个并发调用直接返回 nil。
func isInstallationComplete(destDir string) bool {
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		// 跳过临时文件和未完成的下载包
		if name == "package.zip" || name == "package.zip.tmp" || name == "downloading" {
			continue
		}
		return true
	}
	return false
}

func NewManager(installDir string) *Manager {
	return &Manager{
		client:     &http.Client{Timeout: 30 * time.Minute},
		installDir: installDir,
	}
}

// Cancel 中止指定 key 的下载任务。若任务已结束则无操作。
func (m *Manager) Cancel(key string) {
	if v, ok := m.cancels.LoadAndDelete(key); ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
		}
	}
}

func (m *Manager) tryDownload(ctx context.Context, urls []string, destPath string, callback ProgressCallback) error {
	var lastErr error
	for _, u := range urls {
		if u == "" {
			continue
		}
		err := m.download(ctx, u, destPath, callback)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("所有下载源均失败: %w", lastErr)
}

func (m *Manager) download(ctx context.Context, u, destPath string, callback ProgressCallback) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}

	resp, err := m.client.Do(req)
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

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	lastCallback := time.Now()

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(destPath + ".tmp")
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
			os.Remove(destPath + ".tmp")
			return fmt.Errorf("读取响应失败: %w", err)
		}
	}

	if callback != nil {
		callback(downloaded, total)
	}

	out.Close()

	// 校验实际大小与 Content-Length 一致，捕获传输被静默截断的下载
	if total > 0 && downloaded != total {
		os.Remove(destPath + ".tmp")
		return fmt.Errorf("下载不完整: 实际 %d 字节, 期望 %d 字节", downloaded, total)
	}

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
			fpath, err := safeExtractPath(dest, relative)
			if err != nil {
				return err
			}
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
		fpath, err := safeExtractPath(dest, f.Name)
		if err != nil {
			return err
		}
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

// safeExtractPath 将压缩条目 name 解析到 dest 下的安全路径。
// 若条目含 .. 或解析后逃逸出 dest（zip-slip），返回错误。
func safeExtractPath(dest, name string) (string, error) {
	destClean := filepath.Clean(dest)
	target := filepath.Clean(filepath.Join(destClean, name))
	if target != destClean && !strings.HasPrefix(target, destClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("压缩条目路径逃逸目标目录: %s", name)
	}
	return target, nil
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

// verifyZipIntegrity 做 zip 完整性测试：逐条目全量读取，archive/zip 读取时会校验 CRC32。
// 用于没有登记 SHA256 的包，捕获损坏/被篡改的下载。非 zip 文件跳过（如 composer.phar）。
func verifyZipIntegrity(filePath string) error {
	if !strings.EqualFold(filepath.Ext(filePath), ".zip") {
		return nil
	}
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return fmt.Errorf("压缩包损坏或格式错误: %w", err)
	}
	defer reader.Close()

	buf := make([]byte, 64*1024)
	for _, f := range reader.File {
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("校验压缩条目失败 %s: %w", f.Name, err)
		}
		_, err = io.CopyBuffer(io.Discard, rc, buf)
		rc.Close()
		if err != nil {
			return fmt.Errorf("压缩包条目校验失败 %s: %w", f.Name, err)
		}
	}
	return nil
}

// verifySHA256 计算文件的 SHA256 并与期望值比较。不匹配则返回错误。
func verifySHA256(filePath, expected string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件校验失败: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算校验值失败: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("文件校验失败: 期望 %s，实际 %s", expected, actual)
	}
	return nil
}

func (m *Manager) DownloadAndExtract(url, destDir string, callback ProgressCallback) error {
	return m.TryDownloadAndExtract("download", []string{url}, destDir, callback)
}

// TryDownloadAndExtract 按 key 下载并解压到 destDir。
// key 用于下载进度与取消绑定。
// 同 destDir 的并发调用会被 per-dir mutex 串行化；第二个调用在锁拿到后若发现已安装完成，
// 直接返回 nil——相当于"别人替我装好了"的幂等短路。
// 仅在 destDir 本次新建时才在失败回滚中删除它，避免误删既有安装目录。
// expectedChecksum 为 SHA256 hex；若为空则跳过 SHA256 校验（仍会做 zip CRC32 完整性测试）。
func (m *Manager) TryDownloadAndExtract(key string, urls []string, destDir string, callback ProgressCallback, expectedChecksum ...string) error {
	// per-dir 互斥锁：确保同一 destDir 不会被两个 goroutine 同时写。
	// key 可能不同（"download:xxx" vs "setup-download:xxx"），但 destDir 相同，锁绑定在 destDir 上。
	dirLock := m.perDirLock(destDir)
	dirLock.Lock()
	defer dirLock.Unlock()

	// 幂等短路：锁拿到后若 destDir 已有完整安装内容，说明另一个并发调用已经完成下载+解压，
	// 本次直接返回 nil。destDir 存在但仅有临时文件（package.zip / .tmp）说明下载还在进行中，
	// 等待上面的 dirLock 释放后再判断即可。
	if isInstallationComplete(destDir) {
		return nil
	}

	_, statErr := os.Stat(destDir)
	created := os.IsNotExist(statErr)

	dlCtx, cancel := context.WithCancel(context.Background())
	m.cancels.Store(key, cancel)
	defer func() {
		m.cancels.Delete(key)
		cancel()
	}()

	compensator := tx.NewCompensator()
	txn := tx.NewTransaction("download-extract-" + filepath.Base(destDir))

	var zipPath string

	removeDestDir := compensator.Noop()
	if created {
		removeDestDir = compensator.DeleteDir(destDir)
	}

	txn.
		Step("create-dest-dir", func(ctx context.Context) error {
			return os.MkdirAll(destDir, 0755)
		}, removeDestDir).
		Step("download-package", func(_ context.Context) error {
			zipPath = filepath.Join(destDir, "package.zip")
			return m.tryDownload(dlCtx, urls, zipPath, callback)
		}, compensator.DeleteFile(zipPath)).
		Step("verify-checksum", func(_ context.Context) error {
			if len(expectedChecksum) > 0 && expectedChecksum[0] != "" {
				return verifySHA256(zipPath, expectedChecksum[0])
			}
			// 无校验和时至少做 zip 完整性测试（逐条目读取校验 CRC32），捕获损坏/篡改的包
			return verifyZipIntegrity(zipPath)
		}, compensator.DeleteFile(zipPath)).
		Step("extract-package", func(ctx context.Context) error {
			return m.ExtractZip(zipPath, destDir)
		}, removeDestDir)

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("事务失败: %w", err)
	}

	os.Remove(zipPath)
	return nil
}

func (m *Manager) GetAvailableVersions(serviceType string) ([]ReleaseInfo, error) {
	return m.getVersions(serviceType), nil
}

func (m *Manager) getVersions(serviceType string) []ReleaseInfo {
	pkg := registry.Builtin().Get(serviceType)
	if pkg == nil {
		return nil
	}
	out := make([]ReleaseInfo, len(pkg.Versions))
	for i, v := range pkg.Versions {
		out[i] = ReleaseInfo{
			Version:   v.Version,
			URL:       v.URL,
			MirrorURL: v.Mirror,
			FileName:  v.FileName,
			Checksum:  v.Checksum,
		}
	}
	return out
}
