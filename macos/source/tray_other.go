//go:build !darwin

package main

func (a *App) startTray() {}
func (a *App) stopTray()  {}

func (a *App) showMainWindow() {}
func (a *App) requestQuit()    {}
