package stats

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeAggregatesCacheUsage(t *testing.T) {
	dir := t.TempDir()
	trace := `{ "event": "generation-request", "customMatched": true }
{ "event": "usage", "promptTokens": 1000, "completionTokens": 200, "cacheReadTokens": 700, "cacheWriteTokens": 100 }
`
	if err := os.WriteFile(filepath.Join(dir, "proxy-trace.jsonl"), []byte(trace), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Compute(dir)
	if got.CacheReadTokens != 700 {
		t.Errorf("cache read = %d, want 700", got.CacheReadTokens)
	}
	if got.CacheWriteTokens != 100 {
		t.Errorf("cache write = %d, want 100", got.CacheWriteTokens)
	}
	if got.PromptTokens != 1000 || got.TotalTokens != 1200 {
		t.Errorf("token totals = prompt %d, total %d", got.PromptTokens, got.TotalTokens)
	}
	const wantHitRate = 700.0 / 900.0
	if got.CacheHitRate != wantHitRate {
		t.Errorf("cache hit rate = %f, want %f", got.CacheHitRate, wantHitRate)
	}
}
