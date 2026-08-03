package main

import (
	"context"
	"strings"
	"testing"

	"antigravity-byok/internal/updater"
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

func TestFreshUpdateCacheMessageDoesNotClaimGitHubFailed(t *testing.T) {
	message := cachedUpdateCheckMessage(updater.Info{
		Cached: true, CacheReason: "fresh", CheckedAt: "2026-08-04T12:00:00Z",
		Available: true, LatestVersion: "1.4.4",
	})
	if !strings.Contains(message, "使用最近成功检查结果") || !strings.Contains(message, "v1.4.4") {
		t.Fatalf("fresh cache message = %q", message)
	}
	if strings.Contains(message, "GitHub 暂时无法确认") {
		t.Fatalf("fresh cache message incorrectly reports a network failure: %q", message)
	}
}
