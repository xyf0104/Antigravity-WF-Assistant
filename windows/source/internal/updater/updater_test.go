package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

const testReleaseJSON = `{
  "tag_name":"v1.4.6",
  "html_url":"https://example.test/releases/v1.4.6",
  "published_at":"2026-08-04T00:00:00Z",
  "assets":[
    {"name":"Antigravity-WF-Assistant-macOS-universal-v1.4.6-Installer.pkg","browser_download_url":"https://example.test/macos.pkg","size":123},
    {"name":"Antigravity-WF-Assistant-Windows-x64-v1.4.6-Setup.exe","browser_download_url":"https://example.test/windows.exe","size":456}
  ]
}`

func useLatestReleaseServer(t *testing.T, handler http.Handler) {
	t.Helper()
	server := httptest.NewServer(handler)
	previous := githubLatestReleaseURL
	githubLatestReleaseURL = server.URL
	t.Cleanup(func() {
		githubLatestReleaseURL = previous
		server.Close()
	})
}

func TestCheckWithCacheUsesETagAnd304(t *testing.T) {
	requests := 0
	useLatestReleaseServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			if got := request.Header.Get("If-None-Match"); got != "" {
				t.Fatalf("first request ETag = %q, want empty", got)
			}
			writer.Header().Set("ETag", `"release-1"`)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(testReleaseJSON))
			return
		}
		if got := request.Header.Get("If-None-Match"); got != `"release-1"` {
			t.Fatalf("conditional request ETag = %q", got)
		}
		writer.Header().Set("ETag", `"release-1"`)
		writer.WriteHeader(http.StatusNotModified)
	}))

	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	first, err := CheckWithCache(context.Background(), "", cachePath)
	if err != nil {
		t.Fatalf("first check: %v", err)
	}
	if !first.Available || first.Cached || first.LatestVersion != "1.4.6" || first.CheckedAt == "" {
		t.Fatalf("first check info = %#v", first)
	}
	second, err := CheckWithCache(context.Background(), "", cachePath)
	if err != nil {
		t.Fatalf("fresh cache check: %v", err)
	}
	if !second.Available || !second.Cached || second.CacheReason != "fresh" || second.LatestVersion != "1.4.6" || second.CheckedAt == "" {
		t.Fatalf("fresh cache info = %#v", second)
	}
	if requests != 1 {
		t.Fatalf("fresh cache requests = %d, want 1", requests)
	}

	makeReleaseCacheStale(t, cachePath)
	third, err := CheckWithCache(context.Background(), "", cachePath)
	if err != nil {
		t.Fatalf("conditional check: %v", err)
	}
	if !third.Available || third.Cached || third.LatestVersion != "1.4.6" || third.CheckedAt == "" {
		t.Fatalf("conditional check info = %#v", third)
	}
	if requests != 2 {
		t.Fatalf("stale cache requests = %d, want 2", requests)
	}
}

func makeReleaseCacheStale(t *testing.T, cachePath string) {
	t.Helper()
	cache, ok := loadReleaseCache(cachePath)
	if !ok {
		t.Fatal("release cache was not saved")
	}
	cache.CheckedAt = time.Now().Add(-FreshCacheTTL - time.Minute).UTC().Format(time.RFC3339)
	if err := saveReleaseCache(cachePath, cache); err != nil {
		t.Fatalf("age release cache: %v", err)
	}
}

func TestCheckWithCacheFallsBackAfterServerFailure(t *testing.T) {
	requests := 0
	useLatestReleaseServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			writer.Header().Set("ETag", `"release-1"`)
			_, _ = writer.Write([]byte(testReleaseJSON))
			return
		}
		http.Error(writer, "temporarily unavailable", http.StatusServiceUnavailable)
	}))

	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	if _, err := CheckWithCache(context.Background(), "", cachePath); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	makeReleaseCacheStale(t, cachePath)
	info, err := CheckWithCache(context.Background(), "", cachePath)
	if err != nil {
		t.Fatalf("fallback check: %v", err)
	}
	if !info.Cached || info.CacheReason != "network" || !info.Available || info.LatestVersion != "1.4.6" {
		t.Fatalf("fallback info = %#v", info)
	}
}

func TestCheckWithCacheMarksTimeoutFallback(t *testing.T) {
	requests := 0
	useLatestReleaseServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			writer.Header().Set("ETag", `"release-1"`)
			_, _ = writer.Write([]byte(testReleaseJSON))
			return
		}
		<-request.Context().Done()
	}))

	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	if _, err := CheckWithCache(context.Background(), "", cachePath); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	makeReleaseCacheStale(t, cachePath)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	info, err := CheckWithCache(ctx, "", cachePath)
	if err != nil {
		t.Fatalf("timeout fallback: %v", err)
	}
	if !info.Cached || info.CacheReason != "timeout" || info.LatestVersion != "1.4.6" {
		t.Fatalf("timeout fallback info = %#v", info)
	}
}

func TestCheckWithCacheHonorsCancellationWithoutUsingCache(t *testing.T) {
	started := make(chan struct{})
	requests := 0
	useLatestReleaseServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			writer.Header().Set("ETag", `"release-1"`)
			_, _ = writer.Write([]byte(testReleaseJSON))
			return
		}
		close(started)
		<-request.Context().Done()
	}))
	cachePath := filepath.Join(t.TempDir(), "release-cache.json")
	if _, err := CheckWithCache(context.Background(), "", cachePath); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// Explicit cancellation must win even if the cache is still fresh.
	freshCtx, freshCancel := context.WithCancel(context.Background())
	freshCancel()
	if _, err := CheckWithCache(freshCtx, "", cachePath); !errors.Is(err, context.Canceled) {
		t.Fatalf("fresh-cache cancellation error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("cancelled fresh cache made %d requests, want 1", requests)
	}

	makeReleaseCacheStale(t, cachePath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := CheckWithCache(ctx, "", cachePath)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("update request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled update check did not return promptly")
	}
}

func TestCheckTimeoutStaysShort(t *testing.T) {
	if CheckTimeout != 5*time.Second {
		t.Fatalf("CheckTimeout = %s, want 5s", CheckTimeout)
	}
	if FreshCacheTTL != 10*time.Minute {
		t.Fatalf("FreshCacheTTL = %s, want 10m", FreshCacheTTL)
	}
}
