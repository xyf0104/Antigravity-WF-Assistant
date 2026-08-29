//go:build windows

package main

import (
	_ "embed"
	goruntime "runtime"
	"sync"

	"fyne.io/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var windowsTrayIcon []byte

var windowsTrayState struct {
	sync.RWMutex
	app     *App
	started bool
	ready   bool
}

var showWindowsTrayMainWindow = func(app *App) {
	app.showMainWindow()
}

func handleWindowsTrayPrimaryClick() {
	if app := currentWindowsTrayApp(); app != nil {
		showWindowsTrayMainWindow(app)
	}
}

// startTray creates a real notification-area icon next to the Windows clock.
// It owns its own locked OS thread and message pump: Wails runs its window
// pump elsewhere, so systray.Register alone would display an icon but leave
// its clicks undelivered on some Windows installations.
func (a *App) startTray() {
	windowsTrayState.Lock()
	windowsTrayState.app = a
	if windowsTrayState.started {
		windowsTrayState.Unlock()
		return
	}
	windowsTrayState.started = true
	windowsTrayState.Unlock()

	go runWindowsTray()
}

func runWindowsTray() {
	// A Win32 window receives messages only on the OS thread that created it.
	// Keep the systray loop permanently on this dedicated thread.
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()

	systray.Run(func() {
		windowsTrayState.Lock()
		windowsTrayState.ready = true
		windowsTrayState.Unlock()

		if len(windowsTrayIcon) > 0 {
			systray.SetIcon(windowsTrayIcon)
		}
	systray.SetTooltip("XIASS Tools")
		systray.SetOnTapped(handleWindowsTrayPrimaryClick)
		// With no secondary callback, the maintained systray implementation
		// keeps the native right-click context menu behaviour.
		systray.SetOnSecondaryTapped(nil)
	showItem := systray.AddMenuItem("打开主界面", "显示 XIASS Tools 主窗口")
		systray.AddSeparator()
	quitItem := systray.AddMenuItem("退出 XIASS Tools", "退出助手并释放本地代理端口")

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
	}, func() {
		windowsTrayState.Lock()
		windowsTrayState.ready = false
		windowsTrayState.started = false
		windowsTrayState.Unlock()
	})
}

func (a *App) stopTray() {
	windowsTrayState.Lock()
	if windowsTrayState.app == a {
		windowsTrayState.app = nil
	}
	ready := windowsTrayState.ready
	windowsTrayState.Unlock()
	if ready {
		systray.Quit()
	}
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
	runtime.EventsEmit(a.ctx, "wf:main-window-shown")
}

func (a *App) hideMainWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowHide(a.ctx)
}

func (a *App) quitNativeApplication() {
	runtime.Quit(a.ctx)
}
