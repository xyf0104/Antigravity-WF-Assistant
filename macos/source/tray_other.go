//go:build !darwin

package main

import "github.com/wailsapp/wails/v2/pkg/runtime"

func (a *App) startTray() {}
func (a *App) stopTray()  {}

func (a *App) showMainWindow() {}
func (a *App) hideMainWindow() {}

func (a *App) quitNativeApplication() {
	runtime.Quit(a.ctx)
}
