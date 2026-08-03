package main

import "testing"

func TestCloseIsMinimisedUnlessExplicitExitWasRequested(t *testing.T) {
	app := &App{}
	if !app.shouldMinimiseOnClose() {
		t.Fatal("a normal window close must be minimised")
	}
	app.exitRequested.Store(true)
	if app.shouldMinimiseOnClose() {
		t.Fatal("an explicit exit must be allowed to close the application")
	}
}

func TestQuitAppBeforeStartupIsRejected(t *testing.T) {
	result := (&App{}).QuitApp()
	if result.OK {
		t.Fatal("QuitApp must not report success before Wails supplies its lifecycle context")
	}
}
