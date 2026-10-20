package download

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// buildTestZip 生成一个包含单文件的可下载 zip，用于测试下载+解压链路。
// 顶层目录 singleRoot 会被 ExtractZip 剥除，最终 destDir 下会出现 fileName。
func buildTestZip(t *testing.T, singleRoot, fileName, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, err := w.Create(filepath.ToSlash(filepath.Join(singleRoot, fileName)))
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := io.WriteString(fw, content); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// startZipServer 返回一个模拟下载源的 httptest.Server，hits 记录请求次数。
// delay 用于拉长下载窗口，让第二个 goroutine 有机会观察到锁竞争。
func startZipServer(t *testing.T, body []byte, delay time.Duration, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTryDownloadAndExtract_ConcurrentSameDirSerializesAndShortCircuits
// 覆盖 P0 #5：两个并发调用写同一个 destDir 时，per-dir mutex 串行化 + 幂等短路，
// 让真实下载只发生 1 次，两个调用都拿到成功。
func TestTryDownloadAndExtract_ConcurrentSameDirSerializesAndShortCircuits(t *testing.T) {
	tmp := t.TempDir()
	destDir := filepath.Join(tmp, "programs", "mysql", "8.0.36")
	zipBody := buildTestZip(t, "mysql-8.0.36-winx64", "mysqld.exe", "fake-binary")

	var hits atomic.Int32
	srv := startZipServer(t, zipBody, 150*time.Millisecond, &hits)

	mgr := NewManager(tmp)
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int, key string) {
			defer wg.Done()
			errs[idx] = mgr.TryDownloadAndExtract(key, []string{srv.URL + "/pkg.zip"}, destDir, nil)
		}(i, fmt.Sprintf("download:mysql:8.0.36#%d", i))
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("真实下载次数 = %d, want 1（并发应被锁串行化+幂等短路收敛）", got)
	}
	if _, err := os.Stat(filepath.Join(destDir, "mysqld.exe")); err != nil {
		t.Errorf("安装结果缺失 mysqld.exe: %v", err)
	}
	// 临时文件应被清理，避免 isInstallationComplete 被残留干扰
	if _, err := os.Stat(filepath.Join(destDir, "package.zip")); !os.IsNotExist(err) {
		t.Errorf("残留 package.zip，下载事务未收敛")
	}
}

// TestTryDownloadAndExtract_DifferentDirsIndependent
// 不同 destDir 不应互相阻塞：各自由自己完成一次下载。
func TestTryDownloadAndExtract_DifferentDirsIndependent(t *testing.T) {
	tmp := t.TempDir()
	var hits atomic.Int32
	zipBody := buildTestZip(t, "nginx-1.26.0", "nginx.exe", "fake")
	srv := startZipServer(t, zipBody, 0, &hits)

	mgr := NewManager(tmp)
	dirs := []string{
		filepath.Join(tmp, "programs", "nginx", "1.24.0"),
		filepath.Join(tmp, "programs", "nginx", "1.26.0"),
	}
	var wg sync.WaitGroup
	for _, d := range dirs {
		wg.Add(1)
		go func(dest string) {
			defer wg.Done()
			if err := mgr.TryDownloadAndExtract("dl:"+dest, []string{srv.URL + "/p.zip"}, dest, nil); err != nil {
				t.Errorf("download to %s: %v", dest, err)
			}
		}(d)
	}
	wg.Wait()
	if got := hits.Load(); got != int32(len(dirs)) {
		t.Errorf("下载请求次数 = %d, want %d", got, len(dirs))
	}
}

// TestTryDownload_RetriesAndResumesAfterDrop 模拟 CDN 传一半后断开：
// 第二次请求应带 Range 续传，最终文件完整且只重试一次。
func TestTryDownload_RetriesAndResumesAfterDrop(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 16*1024) // 256KB
	half := len(payload) / 2
	var mu sync.Mutex
	requests := 0
	var secondRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		attempt := requests
		mu.Unlock()

		if attempt == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload[:half])
			w.(http.Flusher).Flush()
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}

		mu.Lock()
		secondRange = r.Header.Get("Range")
		mu.Unlock()
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", half, len(payload)-1, len(payload)))
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)-half))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[half:])
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "package.zip")
	mgr := NewManager(t.TempDir())
	if err := mgr.tryDownload(context.Background(), []string{srv.URL + "/pkg.zip"}, destPath, nil); err != nil {
		t.Fatalf("download should recover from dropped connection: %v", err)
	}
	if requests != 2 {
		t.Errorf("请求次数 = %d, want 2（首包断开后重试一次）", requests)
	}
	if secondRange != fmt.Sprintf("bytes=%d-", half) {
		t.Errorf("续传 Range = %q, want bytes=%d-", secondRange, half)
	}
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("续传后的文件内容不一致: got %d bytes, want %d", len(got), len(payload))
	}
}

// TestTryDownloadFile_NonZipLandsAsFile 覆盖 composer.phar 这类非 zip 包：
// 校验 SHA256 后应直接落盘为指定文件名，不应尝试解压。
func TestTryDownloadFile_NonZipLandsAsFile(t *testing.T) {
	payload := []byte("<?php // fake composer.phar")
	sum := sha256.Sum256(payload)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	destDir := filepath.Join(t.TempDir(), "programs", "composer", "2.8.5")
	mgr := NewManager(t.TempDir())
	if err := mgr.TryDownloadFile("dl:composer", []string{srv.URL}, destDir, "composer.phar", nil, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("TryDownloadFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destDir, "composer.phar"))
	if err != nil {
		t.Fatalf("composer.phar 未落盘: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("落盘内容不一致")
	}
	if _, err := os.Stat(filepath.Join(destDir, "package.zip")); !os.IsNotExist(err) {
		t.Errorf("临时 package.zip 未清理")
	}
}

// TestTryDownload_ServerIgnoresRange：服务器不支持续传而返回 200 时，应整包重下并截断旧内容。
func TestTryDownload_ServerIgnoresRange(t *testing.T) {
	payload := []byte("fresh-payload")
	destPath := filepath.Join(t.TempDir(), "package.zip")
	if err := os.WriteFile(destPath+".tmp", []byte("stale-partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	mgr := NewManager(t.TempDir())
	if err := mgr.tryDownload(context.Background(), []string{srv.URL}, destPath, nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("服务器忽略 Range 时应整包重下，实际内容为 %q", got)
	}
}

// TestTryDownload_NotFoundNotRetried：404 属于永久失败，不应重试不同 URL 之外的同源请求。
func TestTryDownload_NotFoundNotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	mgr := NewManager(t.TempDir())
	err := mgr.tryDownload(context.Background(), []string{srv.URL}, filepath.Join(t.TempDir(), "package.zip"), nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("404 请求次数 = %d, want 1（不应重试）", got)
	}
}

// TestIsInstallationComplete 边界：空目录 / 只有临时文件 / 有真实内容。
func TestIsInstallationComplete(t *testing.T) {
	tmp := t.TempDir()
	if isInstallationComplete(filepath.Join(tmp, "no-such")) {
		t.Errorf("不存在的目录不应视为已完成")
	}
	if isInstallationComplete(tmp) {
		t.Errorf("空目录不应视为已完成")
	}
	for _, name := range []string{"package.zip", "package.zip.tmp", "downloading"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if isInstallationComplete(tmp) {
		t.Errorf("仅有临时文件的目录不应视为已完成")
	}
	if err := os.WriteFile(filepath.Join(tmp, "mysqld.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isInstallationComplete(tmp) {
		t.Errorf("含真实文件的目录应视为已完成")
	}
}

// TestPerDirLock 确保同一 destDir 复用同一把锁，不同 destDir 锁不同。
func TestPerDirLock(t *testing.T) {
	mgr := NewManager(t.TempDir())
	a1 := mgr.perDirLock("/x/a")
	a2 := mgr.perDirLock("/x/a")
	b := mgr.perDirLock("/x/b")
	if a1 != a2 {
		t.Errorf("同 destDir 应返回同一锁实例")
	}
	if a1 == b {
		t.Errorf("不同 destDir 应返回不同锁实例")
	}
}
