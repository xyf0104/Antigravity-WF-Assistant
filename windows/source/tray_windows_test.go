//go:build windows && !wfbridge

package main

import "testing"

func TestWindowsTrayPrimaryClickOpensMainWindow(t *testing.T) {
	fakeApp := &App{}
	windowsTrayState.Lock()
	previousApp := windowsTrayState.app
	windowsTrayState.app = fakeApp
	windowsTrayState.Unlock()
	previousShow := showWindowsTrayMainWindow
	t.Cleanup(func() {
		showWindowsTrayMainWindow = previousShow
		windowsTrayState.Lock()
		windowsTrayState.app = previousApp
		windowsTrayState.Unlock()
	})

	called := false
	showWindowsTrayMainWindow = func(app *App) {
		called = app == fakeApp
	}
	handleWindowsTrayPrimaryClick()
	if !called {
		t.Fatal("primary tray click did not request the main window")
	}
}
