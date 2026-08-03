package main

import (
	"context"
	"testing"
)

func TestApplicationQuitIsOnlyInterceptedUntilNativeExitIsRequested(t *testing.T) {
	app := &App{}
	if !app.beforeClose(context.Background()) {
		t.Fatal("a system quit must be routed through the explicit native exit path")
	}
	app.exitRequested.Store(true)
	if app.beforeClose(context.Background()) {
		t.Fatal("an explicit exit must be allowed to close the application")
	}
}

func TestQuitAppBeforeStartupIsRejected(t *testing.T) {
	result := (&App{}).QuitApp()
	if result.OK {
		t.Fatal("QuitApp must not report success before Wails supplies its lifecycle context")
	}
}
