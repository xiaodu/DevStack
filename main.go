package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIcon []byte

var version = "dev"

// appCtx 保存 Wails 上下文，供 SingleInstanceLock 回调在第二个实例启动时使用。
var appCtx context.Context

// isDevMode 检测是否为开发模式（通过环境变量 WAILS_DEV=1 或 MODE=dev 启用）
func isDevMode() bool {
	return os.Getenv("WAILS_DEV") == "1" || os.Getenv("MODE") == "dev"
}

func main() {
	log.Println("[WAILS] main() started")

	app := NewApp()

	var err error
	err = wails.Run(&options.App{
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "DevStack-{A1B2C3D4-E5F6-7890-ABCD-EF1234567890}",
			OnSecondInstanceLaunch: func(secondInstanceData options.SecondInstanceData) {
				log.Println("[WAILS] 第二个实例已启动，显示已有窗口")
				if appCtx != nil {
					runtime.WindowShow(appCtx)
					runtime.WindowUnminimise(appCtx)
				}
			},
		},
		Title:     "DevStack",
		Width:     1200,
		Height:    800,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// index.html 等入口文件不缓存，避免升级后 WebView2 缓存旧页面导致白屏/无样式；
					// /assets/ 下文件名带内容哈希，可安全长缓存。
					if strings.HasPrefix(r.URL.Path, "/assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					} else {
						w.Header().Set("Cache-Control", "no-store")
					}
					next.ServeHTTP(w, r)
				})
			},
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 255},
		OnStartup:        app.startup,
		OnShutdown: func(ctx context.Context) {
			app.Shutdown()
		},
		OnBeforeClose: func(ctx context.Context) bool {
			return app.HandleWindowClose(ctx)
		},
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
		Debug: options.Debug{
			OpenInspectorOnStartup: isDevMode(),
		},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			Appearance:           mac.NSAppearanceNameDarkAqua,
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
		Linux: &linux.Options{
			ProgramName: "DevStack",
		},
	})

	if err != nil {
		log.Println("[WAILS] wails.Run error:", err.Error())
		println("Error:", err.Error())
	}
	log.Println("[WAILS] main() exited")
}
