package main

import (
	_ "embed"
	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

func (a *App) startTray() {
	go systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTitle("LumeDAV")
		systray.SetTooltip("LumeDAV WebDAV 服务")
		show := systray.AddMenuItem("打开 LumeDAV", "显示主窗口")
		toggle := systray.AddMenuItem("启动 / 停止服务", "切换服务状态")
		systray.AddSeparator()
		quit := systray.AddMenuItem("退出", "停止服务并退出")
		go func() {
			for {
				select {
				case <-show.ClickedCh:
					runtime.WindowShow(a.ctx)
					runtime.WindowUnminimise(a.ctx)
				case <-toggle.ClickedCh:
					a.mu.Lock()
					if a.server == nil {
						_ = a.startLocked()
					} else {
						_ = a.stopLocked()
					}
					a.mu.Unlock()
				case <-quit.ClickedCh:
					a.mu.Lock()
					a.quitting = true
					a.mu.Unlock()
					systray.Quit()
					runtime.Quit(a.ctx)
					return
				}
			}
		}()
	}, func() {})
}
