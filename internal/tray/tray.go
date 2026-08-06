package tray

import (
	"context"
	"sync/atomic"

	systray "github.com/getlantern/systray"
)

// Tray 封装系统托盘图标：关闭窗口时隐藏到托盘，从托盘恢复主窗口或退出应用。
type Tray struct {
	ctx     context.Context
	showFn  func()
	quitFn  func()
	running atomic.Bool
}

// New 创建托盘实例。showFn 在点击"显示主窗口"时调用，quitFn 在点击"退出"时调用。
func New(showFn, quitFn func()) *Tray {
	return &Tray{showFn: showFn, quitFn: quitFn}
}

// SetContext 在应用启动后注入 Wails 上下文。
func (t *Tray) SetContext(ctx context.Context) {
	t.ctx = ctx
}

// Running 返回托盘消息循环是否已就绪（图标已显示）。
func (t *Tray) Running() bool {
	return t.running.Load()
}

// Start 在独立 goroutine 中启动托盘消息循环。iconBytes 应为 .ico 文件内容。
func (t *Tray) Start(iconBytes []byte) {
	go systray.Run(func() {
		t.running.Store(true)
		if len(iconBytes) > 0 {
			systray.SetIcon(iconBytes)
		}
		systray.SetTitle("DevStack")
		systray.SetTooltip("DevStack 开发环境管理器")

		mShow := systray.AddMenuItem("显示主窗口", "显示 DevStack 主窗口")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 DevStack")

		go func() {
			for {
				select {
				case <-mShow.ClickedCh:
					if t.showFn != nil {
						t.showFn()
					}
				case <-mQuit.ClickedCh:
					if t.quitFn != nil {
						t.quitFn()
					}
					return
				}
			}
		}()
	}, func() {
		t.running.Store(false)
	})
}

// Quit 退出托盘消息循环并移除图标。
func (t *Tray) Quit() {
	if t.running.Load() {
		systray.Quit()
	}
}
