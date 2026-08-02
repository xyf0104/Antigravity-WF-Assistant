package stats

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

// Stats aggregates usage from proxy-trace.jsonl.
type Stats struct {
	TotalRequests    int     `json:"totalRequests"`
	CustomRequests   int     `json:"customRequests"`
	TotalTokens      int     `json:"totalTokens"`
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	CacheReadTokens  int     `json:"cacheReadTokens"`
	CacheWriteTokens int     `json:"cacheWriteTokens"`
	CacheHitRate     float64 `json:"cacheHitRate"`
}

// Compute reads proxy-trace.jsonl and returns aggregated stats.
func Compute(storageDir string) Stats {
	path := filepath.Join(storageDir, "proxy-trace.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return Stats{}
	}
	defer f.Close()

	var s Stats
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 2*1024*1024), 2*1024*1024)
	for scanner.Scan() {
		var ev map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		evType, _ := ev["event"].(string)
		switch evType {
		case "generation-request":
			s.TotalRequests++
			if matched, _ := ev["customMatched"].(bool); matched {
				s.CustomRequests++
			}
		case "usage":
			if p, ok := ev["promptTokens"].(float64); ok {
				s.PromptTokens += int(p)
				s.TotalTokens += int(p)
			}
			if c, ok := ev["completionTokens"].(float64); ok {
				s.CompletionTokens += int(c)
				s.TotalTokens += int(c)
			}
			if cr, ok := ev["cacheReadTokens"].(float64); ok {
				s.CacheReadTokens += int(cr)
			}
			if cw, ok := ev["cacheWriteTokens"].(float64); ok {
				s.CacheWriteTokens += int(cw)
			}
		}
	}

	// Calculate cache hit rate: cacheRead / (cacheRead + non-cache input)
	nonCacheInput := s.PromptTokens - s.CacheReadTokens - s.CacheWriteTokens
	if nonCacheInput < 0 {
		nonCacheInput = 0
	}
	denom := s.CacheReadTokens + nonCacheInput
	if denom > 0 {
		s.CacheHitRate = float64(s.CacheReadTokens) / float64(denom)
	}

	return s
}
