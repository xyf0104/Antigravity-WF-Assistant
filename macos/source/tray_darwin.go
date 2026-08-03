//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>

void wfTrayStart(const unsigned char *iconBytes, int iconLength);
void wfTrayStop(void);
*/
import "C"

import (
	_ "embed"
	"sync"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/appicon.png
var darwinTrayIcon []byte

var darwinTrayState struct {
	sync.RWMutex
	app *App
}

// startTray adds a native macOS menu-bar item without changing Wails' own
// NSApplication delegate. The menu remains available while the main window is
// minimised to the Dock.
func (a *App) startTray() {
	darwinTrayState.Lock()
	darwinTrayState.app = a
	darwinTrayState.Unlock()

	if len(darwinTrayIcon) == 0 {
		return
	}
	C.wfTrayStart((*C.uchar)(unsafe.Pointer(&darwinTrayIcon[0])), C.int(len(darwinTrayIcon)))
}

func (a *App) stopTray() {
	darwinTrayState.Lock()
	if darwinTrayState.app == a {
		darwinTrayState.app = nil
	}
	darwinTrayState.Unlock()
	C.wfTrayStop()
}

func darwinTrayApp() *App {
	darwinTrayState.RLock()
	defer darwinTrayState.RUnlock()
	return darwinTrayState.app
}

//export wfTrayShowWindow
func wfTrayShowWindow() {
	if app := darwinTrayApp(); app != nil {
		app.showMainWindow()
	}
}

//export wfTrayQuitApplication
func wfTrayQuitApplication() {
	if app := darwinTrayApp(); app != nil {
		app.requestQuit()
	}
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
