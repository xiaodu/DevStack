package download

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
