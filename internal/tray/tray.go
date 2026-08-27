package tray

import (
	"context"
	"sync/atomic"
	"time"

	systray "github.com/getlantern/systray"
)

// quitWaitTimeout 等待托盘消息循环退出（图标移除）的超时时间。
const quitWaitTimeout = 2 * time.Second

// Tray 封装系统托盘图标：关闭窗口时隐藏到托盘，从托盘恢复主窗口或退出应用。
type Tray struct {
	ctx     context.Context
	showFn  func()
	quitFn  func()
	running atomic.Bool // 托盘消息循环是否在运行
	ready   atomic.Bool // 托盘窗口是否已创建、图标是否已显示
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
	if !t.running.CompareAndSwap(false, true) {
		return
	}
	t.ready.Store(false)
	go systray.Run(func() {
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

		// onReady 由库在托盘窗口创建完成后触发，此时 Quit 投递的 WM_CLOSE 才有归宿。
		t.ready.Store(true)
	}, func() {
		t.ready.Store(false)
		t.running.Store(false)
	})
}

// Quit 退出托盘消息循环并移除图标。
//
// 注意：systray.Quit() 仅向托盘窗口异步投递 WM_CLOSE，
// 真正的 Shell_NotifyIcon(NIM_DELETE) 在消息循环处理 WM_DESTROY 时执行，
// 完成后才回调 onExit（running=false）。这里必须等待其完成后再返回，
// 否则进程先于图标移除退出，通知区域会残留"幽灵图标"（悬停才消失）。
func (t *Tray) Quit() {
	if !t.ready.CompareAndSwap(true, false) {
		return
	}
	systray.Quit()

	deadline := time.Now().Add(quitWaitTimeout)
	for t.running.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	t.running.Store(false)
}
