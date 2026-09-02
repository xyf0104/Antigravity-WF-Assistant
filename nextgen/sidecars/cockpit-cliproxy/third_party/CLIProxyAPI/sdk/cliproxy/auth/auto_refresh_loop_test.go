package auth

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type testRefreshEvaluator struct{}

func (testRefreshEvaluator) ShouldRefresh(time.Time, *Auth) bool { return false }

func setRefreshLeadFactory(t *testing.T, provider string, factory func() *time.Duration) {
	t.Helper()
	key := strings.ToLower(strings.TrimSpace(provider))
	refreshLeadMu.Lock()
	prev, hadPrev := refreshLeadFactories[key]
	if factory == nil {
		delete(refreshLeadFactories, key)
	} else {
		refreshLeadFactories[key] = factory
	}
	refreshLeadMu.Unlock()
	t.Cleanup(func() {
		refreshLeadMu.Lock()
		if hadPrev {
			refreshLeadFactories[key] = prev
		} else {
			delete(refreshLeadFactories, key)
		}
		refreshLeadMu.Unlock()
	})
}

func TestNextRefreshCheckAt_DisabledUnschedule(t *testing.T) {
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(time.Hour)
	lead := 10 * time.Minute
	setRefreshLeadFactory(t, "disabled-schedule", func() *time.Duration {
		d := lead
		return &d
	})

	auth := &Auth{
		ID:       "a1",
		Provider: "disabled-schedule",
		Disabled: true,
		Status:   StatusDisabled,
		Metadata: map[string]any{
			"email":      "x@example.com",
			"expires_at": expiry.Format(time.RFC3339),
		},
	}

	got, ok := nextRefreshCheckAt(now, auth, 15*time.Minute)
	if !ok {
		t.Fatalf("nextRefreshCheckAt() ok = false, want true")
	}
	want := expiry.Add(-lead)
	if !got.Equal(want) {
		t.Fatalf("nextRefreshCheckAt() = %s, want %s", got, want)
	}
}

func TestNextRefreshCheckAt_APIKeyUnschedule(t *testing.T) {
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	auth := &Auth{ID: "a1", Provider: "test", Attributes: map[string]string{"api_key": "k"}}
	if _, ok := nextRefreshCheckAt(now, auth, 15*time.Minute); ok {
		t.Fatalf("nextRefreshCheckAt() ok = true, want false")
	}
}

func TestStaticAccessTokenAuthDoesNotEnterAutoRefresh(t *testing.T) {
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	lead := 10 * time.Minute
	setRefreshLeadFactory(t, "static-access-token", func() *time.Duration {
		d := lead
		return &d
	})
	auth := &Auth{
		ID:       "a1",
		Provider: "static-access-token",
		Attributes: map[string]string{
			AttributeAuthKind: "access_token",
		},
		Metadata: map[string]any{
			"access_token":     "pat-token",
			"openai_auth_mode": "personal_access_token",
		},
	}

	if _, ok := nextRefreshCheckAt(now, auth, 15*time.Minute); ok {
		t.Fatal("static access token must not be scheduled for automatic refresh")
	}
	if (&Manager{}).shouldRefresh(auth, now) {
		t.Fatal("static access token must not be refreshed")
	}
}

func TestNextRefreshCheckAt_NextRefreshAfterGate(t *testing.T) {
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	nextAfter := now.Add(30 * time.Minute)
	auth := &Auth{
		ID:               "a1",
		Provider:         "test",
		NextRefreshAfter: nextAfter,
		Metadata:         map[string]any{"email": "x@example.com"},
	}
	got, ok := nextRefreshCheckAt(now, auth, 15*time.Minute)
	if !ok {
		t.Fatalf("nextRefreshCheckAt() ok = false, want true")
	}
	if !got.Equal(nextAfter) {
		t.Fatalf("nextRefreshCheckAt() = %s, want %s", got, nextAfter)
	}
}

func TestNextRefreshCheckAt_PreferredInterval_PicksEarliestCandidate(t *testing.T) {
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(20 * time.Minute)
	auth := &Auth{
		ID:              "a1",
		Provider:        "test",
		LastRefreshedAt: now,
		Metadata: map[string]any{
			"email":                    "x@example.com",
			"expires_at":               expiry.Format(time.RFC3339),
			"refresh_interval_seconds": 900, // 15m
		},
	}
	got, ok := nextRefreshCheckAt(now, auth, 15*time.Minute)
	if !ok {
		t.Fatalf("nextRefreshCheckAt() ok = false, want true")
	}
	want := expiry.Add(-15 * time.Minute)
	if !got.Equal(want) {
		t.Fatalf("nextRefreshCheckAt() = %s, want %s", got, want)
	}
}

func TestNextRefreshCheckAt_ProviderLead_Expiry(t *testing.T) {
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(time.Hour)
	lead := 10 * time.Minute
	setRefreshLeadFactory(t, "provider-lead-expiry", func() *time.Duration {
		d := lead
		return &d
	})

	auth := &Auth{
		ID:       "a1",
		Provider: "provider-lead-expiry",
		Metadata: map[string]any{
			"email":      "x@example.com",
			"expires_at": expiry.Format(time.RFC3339),
		},
	}

	got, ok := nextRefreshCheckAt(now, auth, 15*time.Minute)
	if !ok {
		t.Fatalf("nextRefreshCheckAt() ok = false, want true")
	}
	want := expiry.Add(-lead)
	if !got.Equal(want) {
		t.Fatalf("nextRefreshCheckAt() = %s, want %s", got, want)
	}
}

func TestNextRefreshCheckAt_RefreshEvaluatorFallback(t *testing.T) {
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	interval := 15 * time.Minute
	auth := &Auth{
		ID:       "a1",
		Provider: "test",
		Metadata: map[string]any{"email": "x@example.com"},
		Runtime:  testRefreshEvaluator{},
	}
	got, ok := nextRefreshCheckAt(now, auth, interval)
	if !ok {
		t.Fatalf("nextRefreshCheckAt() ok = false, want true")
	}
	want := now.Add(interval)
	if !got.Equal(want) {
		t.Fatalf("nextRefreshCheckAt() = %s, want %s", got, want)
	}
}

type blockingAutoRefreshExecutor struct {
	id      string
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (e *blockingAutoRefreshExecutor) Identifier() string { return e.id }

func (*blockingAutoRefreshExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (*blockingAutoRefreshExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (e *blockingAutoRefreshExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	e.once.Do(func() { close(e.started) })
	<-e.release
	return auth.Clone(), nil
}

func (*blockingAutoRefreshExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (*blockingAutoRefreshExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestStopAutoRefreshAndWaitWaitsForInFlightWorker(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseWorker)

	executor := &blockingAutoRefreshExecutor{
		id:      "blocking-auto-refresh",
		started: make(chan struct{}),
		release: release,
	}
	manager.RegisterExecutor(executor)
	auth := &Auth{
		ID:       "blocking-refresh-auth",
		Provider: executor.id,
		Attributes: map[string]string{
			AttributeAuthKind: AuthKindOAuth,
		},
		Metadata: map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
		},
	}
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	t.Cleanup(runtimeCancel)
	loop := newAuthAutoRefreshLoop(manager, time.Hour, 1)
	manager.mu.Lock()
	manager.refreshCancel = runtimeCancel
	manager.refreshLoop = loop
	manager.mu.Unlock()
	go loop.run(runtimeCtx)
	loop.jobs <- auth.ID

	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("auto-refresh worker did not enter Refresh")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	stopped := make(chan bool, 1)
	go func() {
		stopped <- manager.StopAutoRefreshAndWait(stopCtx)
	}()

	select {
	case stoppedEarly := <-stopped:
		t.Fatalf("StopAutoRefreshAndWait returned before the refresh worker exited: %v", stoppedEarly)
	case <-time.After(50 * time.Millisecond):
	}

	releaseWorker()
	select {
	case stoppedCleanly := <-stopped:
		if !stoppedCleanly {
			t.Fatal("StopAutoRefreshAndWait timed out after the refresh worker exited")
		}
	case <-stopCtx.Done():
		t.Fatalf("StopAutoRefreshAndWait did not wait for refresh worker shutdown: %v", stopCtx.Err())
	}
}
