//go:build windows

package main

import (
	_ "embed"
	"sync"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var windowsTrayIcon []byte

var windowsTrayState struct {
	sync.RWMutex
	app     *App
	started bool
}

// startTray registers a Windows notification-area icon. Register integrates
// with Wails' existing Windows event loop, so closing the main window can keep
// the proxy alive and the user can reopen or exit it from the tray menu.
func (a *App) startTray() {
	windowsTrayState.Lock()
	windowsTrayState.app = a
	if windowsTrayState.started {
		windowsTrayState.Unlock()
		return
	}
	windowsTrayState.started = true
	windowsTrayState.Unlock()

	systray.Register(func() {
		if len(windowsTrayIcon) > 0 {
			systray.SetIcon(windowsTrayIcon)
		}
		systray.SetTooltip("Antigravity WF助手")
		showItem := systray.AddMenuItem("打开主界面", "显示 Antigravity WF助手主窗口")
		systray.AddSeparator()
		quitItem := systray.AddMenuItem("退出 Antigravity WF助手", "退出助手并释放本地代理端口")

		go func() {
			for {
				select {
				case <-showItem.ClickedCh:
					if app := currentWindowsTrayApp(); app != nil {
						app.showMainWindow()
					}
				case <-quitItem.ClickedCh:
					if app := currentWindowsTrayApp(); app != nil {
						app.requestQuit()
					}
					return
				}
			}
		}()
	}, func() {})
}

func (a *App) stopTray() {
	windowsTrayState.Lock()
	if windowsTrayState.app == a {
		windowsTrayState.app = nil
	}
	windowsTrayState.Unlock()
	systray.Quit()
}

func currentWindowsTrayApp() *App {
	windowsTrayState.RLock()
	defer windowsTrayState.RUnlock()
	return windowsTrayState.app
}

func (a *App) showMainWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

func (a *App) requestQuit() {
	if a.ctx == nil {
		return
	}
	a.exitRequested.Store(true)
	runtime.Quit(a.ctx)
}
