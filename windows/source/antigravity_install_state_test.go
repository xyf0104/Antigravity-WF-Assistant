package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"antigravity-byok/internal/patcher"
)

func TestAntigravityProductRepatchStateTracksVersionAndExecutable(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "Antigravity IDE.exe")
	if err := os.WriteFile(executable, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{storageDir: dir}
	target := patcher.TargetStatus{Kind: "ide", AppPath: dir, ExecutablePath: executable, Version: "2.5.5"}
	record := antigravityInstallRecordFromTarget(target)
	record.PatchRevision = antigravityPatchRevision
	baseline := antigravityInstallState{Schema: 1, Targets: []antigravityInstallRecord{record}}
	if err := app.saveAntigravityInstallState(baseline); err != nil {
		t.Fatal(err)
	}
	if required, message := app.antigravityProductRepatchState(patcher.Status{Targets: []patcher.TargetStatus{target}}); required || message != "" {
		t.Fatalf("unchanged target requested reconnect: required=%t message=%q", required, message)
	}

	target.Version = "2.6.0"
	if required, message := app.antigravityProductRepatchState(patcher.Status{Targets: []patcher.TargetStatus{target}}); !required || !strings.Contains(message, "2.6.0") {
		t.Fatalf("version change not detected: required=%t message=%q", required, message)
	}

	target.Version = "2.5.5"
	if err := os.WriteFile(executable, []byte("second-build"), 0o600); err != nil {
		t.Fatal(err)
	}
	if required, message := app.antigravityProductRepatchState(patcher.Status{Targets: []patcher.TargetStatus{target}}); !required || !strings.Contains(message, "程序文件") {
		t.Fatalf("executable change not detected: required=%t message=%q", required, message)
	}
}

func TestAntigravityProductRepatchStateRequiresCurrentPatchRevision(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "Antigravity IDE.exe")
	if err := os.WriteFile(executable, []byte("same-build"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{storageDir: dir}
	target := patcher.TargetStatus{Kind: "ide", AppPath: dir, ExecutablePath: executable, Version: "2.5.5"}
	legacy := antigravityInstallState{Schema: 1, Targets: []antigravityInstallRecord{antigravityInstallRecordFromTarget(target)}}
	if err := app.saveAntigravityInstallState(legacy); err != nil {
		t.Fatal(err)
	}
	if required, message := app.antigravityProductRepatchState(patcher.Status{Targets: []patcher.TargetStatus{target}}); !required || !strings.Contains(message, "旧版连接规则") {
		t.Fatalf("legacy helper rule did not request reconnect: required=%t message=%q", required, message)
	}

	record := antigravityInstallRecordFromTarget(target)
	record.PatchRevision = antigravityPatchRevision
	if err := app.saveAntigravityInstallState(antigravityInstallState{Schema: 1, Targets: []antigravityInstallRecord{record}}); err != nil {
		t.Fatal(err)
	}
	if required, message := app.antigravityProductRepatchState(patcher.Status{Targets: []patcher.TargetStatus{target}}); required || message != "" {
		t.Fatalf("current helper rule requested reconnect: required=%t message=%q", required, message)
	}
}

func TestHistorySyncIsRecentRequiresSuccessfulFreshRun(t *testing.T) {
	app := &App{historyStatus: HistorySyncStatus{State: "success", LastRunAt: time.Now().Add(-30 * time.Second).Format(time.RFC3339)}}
	if !app.historySyncIsRecent(2 * time.Minute) {
		t.Fatal("fresh successful history sync was not reused")
	}
	app.historyStatus.LastRunAt = time.Now().Add(-3 * time.Minute).Format(time.RFC3339)
	if app.historySyncIsRecent(2 * time.Minute) {
		t.Fatal("stale history sync was reused")
	}
	app.historyStatus = HistorySyncStatus{State: "error", LastRunAt: time.Now().Format(time.RFC3339)}
	if app.historySyncIsRecent(2 * time.Minute) {
		t.Fatal("failed history sync was reused")
	}
}
