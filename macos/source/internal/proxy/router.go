package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"antigravity-byok/internal/storage"
)

const (
	googleHost    = "daily-cloudcode-pa.googleapis.com"
	googleBaseURL = "https://daily-cloudcode-pa.googleapis.com"
	maxRetries    = 3
	// Antigravity 1.23.x only recognises placeholder enum values M0-M150.
	// Unknown values are decoded as MODEL_UNSPECIFIED and disappear from the
	// built-in model picker.
	modelPlaceholderCount = 151
)

var (
	placeholderMu         sync.RWMutex
	allocatedPlaceholders = map[string]string{}
)

// getModelSlug returns a stable routing slug for a model.
func getModelSlug(m storage.CustomModel) string {
	src := m.ExternalModelName
	if src == "" {
		src = m.Name
	}
	src = strings.TrimPrefix(src, "models/")
	var b strings.Builder
	for _, r := range strings.ToLower(src) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "model"
	}
	return "custom-" + slug
}

func modelPlaceholderKey(m storage.CustomModel) string {
	if m.Name != "" {
		return m.Name
	}
	if m.ExternalModelName != "" {
		return m.ExternalModelName
	}
	return m.DisplayName
}

func modelPlaceholderHash(m storage.CustomModel) uint32 {
	src := strings.ToLower(m.DisplayName)
	if src == "" {
		src = strings.ToLower(modelPlaceholderKey(m))
	}
	var h uint32 = 5381
	for _, c := range src {
		h = (h << 5) + h + uint32(c)
	}
	return h
}

// getModelPlaceholder returns the placeholder allocated during the latest
// model-list injection, with a valid deterministic fallback for unit-level
// conversion calls.
func getModelPlaceholder(m storage.CustomModel) string {
	key := modelPlaceholderKey(m)
	placeholderMu.RLock()
	placeholder := allocatedPlaceholders[key]
	placeholderMu.RUnlock()
	if placeholder != "" {
		return placeholder
	}
	return fmt.Sprintf("MODEL_PLACEHOLDER_M%d", modelPlaceholderHash(m)%modelPlaceholderCount)
}

// allocateModelPlaceholders selects valid enum values that do not collide with
// models already present in Google's response. Assignments are kept in memory
// so subsequent generation requests can be routed back to the matching BYOK
// model.
func allocateModelPlaceholders(models []storage.CustomModel, officialModels map[string]any) map[string]string {
	used := make(map[string]struct{})
	for _, raw := range officialModels {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if modelID, ok := entry["model"].(string); ok && modelID != "" {
			used[modelID] = struct{}{}
		}
	}

	ordered := append([]storage.CustomModel(nil), models...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return modelPlaceholderKey(ordered[i]) < modelPlaceholderKey(ordered[j])
	})

	assignments := make(map[string]string, len(ordered))
	for _, model := range ordered {
		start := int(modelPlaceholderHash(model) % modelPlaceholderCount)
		for offset := 0; offset < modelPlaceholderCount; offset++ {
			candidate := fmt.Sprintf("MODEL_PLACEHOLDER_M%d", (start+offset)%modelPlaceholderCount)
			if _, exists := used[candidate]; exists {
				continue
			}
			assignments[modelPlaceholderKey(model)] = candidate
			used[candidate] = struct{}{}
			break
		}
	}

	placeholderMu.Lock()
	allocatedPlaceholders = assignments
	placeholderMu.Unlock()
	return assignments
}

// buildFakeModelEntry builds the JSON entry injected into the model list.
func buildFakeModelEntry(m storage.CustomModel, placeholder string) map[string]any {
	return map[string]any{
		"displayName":                  m.DisplayName,
		"description":                  m.Description,
		"recommended":                  true,
		"maxTokens":                    1048576,
		"maxOutputTokens":              65536,
		"tokenizerType":                "LLAMA_WITH_SPECIAL",
		"model":                        placeholder,
		"apiProvider":                  "API_PROVIDER_GOOGLE_GEMINI",
		"modelProvider":                "MODEL_PROVIDER_GOOGLE",
		"supportsCumulativeContext":    true,
		"supportsEstimateTokenCounter": true,
		"supportsImages":               true,
		"supportsVideo":                false,
	}
}

// handleFetchAvailableModels proxies fetchAvailableModels and injects custom models.
func handleFetchAvailableModels(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, googleBaseURL+"/v1internal:fetchAvailableModels", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Host", googleHost)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}

	// Decompress if needed
	var decoded []byte
	enc := resp.Header.Get("Content-Encoding")
	if enc == "gzip" {
		gr, err := gzip.NewReader(bytes.NewReader(respBody))
		if err == nil {
			decoded, _ = io.ReadAll(gr)
			gr.Close()
		}
	}
	if decoded == nil {
		decoded = respBody
	}

	var parsed map[string]any
	if err := json.Unmarshal(decoded, &parsed); err != nil {
		// Forward raw on parse failure
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	models, _ := storage.LoadModels()
	injectedCount := 0
	officialCount := 0
	var injectedNames []string

	// Inject into modelMap (map shape)
	modelMapRaw, hasMap := parsed["models"]
	if hasMap {
		if modelMap, ok := modelMapRaw.(map[string]any); ok {
			officialCount = len(modelMap)
			placeholders := allocateModelPlaceholders(models, modelMap)
			for _, m := range models {
				placeholder := placeholders[modelPlaceholderKey(m)]
				if placeholder == "" {
					continue
				}
				slug := getModelSlug(m)
				modelMap[slug] = buildFakeModelEntry(m, placeholder)
				addAgentModelID(&parsed, slug)
				injectedCount++
				injectedNames = append(injectedNames, m.DisplayName)
			}
		}
	} else {
		// Build map from scratch if missing
		newMap := map[string]any{}
		placeholders := allocateModelPlaceholders(models, newMap)
		for _, m := range models {
			placeholder := placeholders[modelPlaceholderKey(m)]
			if placeholder == "" {
				continue
			}
			slug := getModelSlug(m)
			newMap[slug] = buildFakeModelEntry(m, placeholder)
			addAgentModelID(&parsed, slug)
			injectedCount++
			injectedNames = append(injectedNames, m.DisplayName)
		}
		if len(newMap) > 0 {
			parsed["models"] = newMap
		}
	}

	out, _ := json.Marshal(parsed)
	outHeaders := make(http.Header)
	for k, vs := range resp.Header {
		if strings.ToLower(k) != "content-encoding" &&
			strings.ToLower(k) != "content-length" &&
			strings.ToLower(k) != "transfer-encoding" {
			outHeaders[k] = vs
		}
	}
	outHeaders.Set("Content-Type", "application/json; charset=utf-8")
	for k, vs := range outHeaders {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(out)

	trace("models-injected", map[string]any{
		"officialCount": officialCount,
		"customCount":   injectedCount,
		"customNames":   injectedNames,
	})
}

func addAgentModelID(parsed *map[string]any, modelID string) {
	sorts, _ := (*parsed)["agentModelSorts"].([]any)
	if len(sorts) == 0 {
		sorts = []any{map[string]any{
			"displayName": "Custom",
			"groups":      []any{map[string]any{"modelIds": []any{}}},
		}}
	}
	sort0, ok := sorts[0].(map[string]any)
	if !ok {
		return
	}
	groups, _ := sort0["groups"].([]any)
	if len(groups) == 0 {
		groups = []any{map[string]any{"modelIds": []any{}}}
	}
	group0, ok := groups[0].(map[string]any)
	if !ok {
		return
	}
	ids, _ := group0["modelIds"].([]any)
	for _, id := range ids {
		if id == modelID {
			return
		}
	}
	group0["modelIds"] = append([]any{modelID}, ids...)
	groups[0] = group0
	sort0["groups"] = groups
	sorts[0] = sort0
	(*parsed)["agentModelSorts"] = sorts
}

// findModel returns the custom model matching a model ID or placeholder.
func findModel(modelID string) *storage.CustomModel {
	models, _ := storage.LoadModels()
	for _, m := range models {
		slug := getModelSlug(m)
		placeholder := getModelPlaceholder(m)
		if modelID == m.Name || modelID == m.ExternalModelName ||
			modelID == slug || modelID == placeholder ||
			modelID == strings.TrimPrefix(m.Name, "models/") {
			mc := m
			return &mc
		}
	}
	return nil
}

// handleGenerate routes a streamGenerateContent request. cleanPath is the
// already-normalised path, with any patcher prefix removed.
func handleGenerate(w http.ResponseWriter, r *http.Request, cleanPath string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}

	modelID, _ := req["model"].(string)
	if modelID == "" {
		if mid, ok := req["modelId"].(string); ok {
			modelID = mid
		}
	}

	requestID, _ := req["requestId"].(string)
	if requestID == "" {
		requestID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	customModel := findModel(modelID)

	trace("generation-request", map[string]any{
		"requestId":     requestID,
		"model":         modelID,
		"customMatched": customModel != nil,
	})

	if customModel == nil {
		// Passthrough to Google
		passthroughRequest(w, r, body, cleanPath)
		return
	}

	geminiReq, _ := req["request"].(map[string]any)
	if geminiReq == nil {
		geminiReq = req
	}

	if customModel.Provider == "anthropic" {
		forwardAnthropic(w, r, customModel, geminiReq, requestID)
	} else {
		forwardOpenAI(w, r, customModel, geminiReq, requestID)
	}
}

// forwardOpenAI translates and forwards to an OpenAI-compatible API.
func forwardOpenAI(w http.ResponseWriter, incoming *http.Request, m *storage.CustomModel, geminiReq map[string]any, requestID string) {
	openAIReq := toOpenAIRequest(geminiReq, m.ExternalModelName)
	if m.ReasoningEffort != "" && m.ReasoningEffort != "auto" {
		openAIReq["reasoning_effort"] = m.ReasoningEffort
		delete(openAIReq, "temperature")
	}
	openAIReq["stream"] = true
	openAIReq["stream_options"] = map[string]any{"include_usage": true}
	cache := applyOpenAIPromptCaching(openAIReq, m, geminiReq)
	cacheEnabled := true

	apiURL := resolveOpenAIChatCompletionsURL(m.APIURL)

	var lastErr error
	extraAttempts := 0
	for attempt := 1; attempt <= maxRetries+extraAttempts; attempt++ {
		body, _ := json.Marshal(openAIReq)
		trace("openai-upstream-request", map[string]any{
			"requestId": requestID, "attempt": attempt,
			"promptCache": cacheEnabled, "promptCacheExplicit": cacheEnabled && cache.explicit,
			"promptCacheKeyHash": strings.TrimPrefix(cache.key, "antigravity:"),
		})
		req, err := http.NewRequestWithContext(incoming.Context(), "POST", apiURL, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+m.APIKey)

		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			trace("openai-upstream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			if attempt < maxRetries+extraAttempts && incoming.Context().Err() == nil {
				delay := retryDelay(attempt, "")
				trace("openai-upstream-retry", map[string]any{"requestId": requestID, "attempt": attempt, "delayMs": delay.Milliseconds()})
				time.Sleep(delay)
				continue
			}
			break
		}
		lastErr = nil

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			trace("openai-upstream-error-response", map[string]any{
				"requestId":  requestID,
				"statusCode": resp.StatusCode,
				"body":       string(errBody[:min(len(errBody), 500)]),
			})
			if cacheEnabled && isUnsupportedCacheResponse(resp.StatusCode, string(errBody)) {
				stripOpenAIPromptCaching(openAIReq)
				cacheEnabled = false
				extraAttempts = 1
				trace("prompt-cache-fallback", map[string]any{
					"requestId": requestID, "provider": "openai", "statusCode": resp.StatusCode,
				})
				continue
			}
			if isRetryableStatus(resp.StatusCode) && attempt < maxRetries+extraAttempts {
				delay := retryDelay(attempt, resp.Header.Get("Retry-After"))
				trace("openai-upstream-retry", map[string]any{"requestId": requestID, "attempt": attempt, "statusCode": resp.StatusCode, "delayMs": delay.Milliseconds()})
				time.Sleep(delay)
				continue
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			w.Write(errBody)
			return
		}

		defer resp.Body.Close()
		streamOpenAIResponse(w, resp, requestID, attempt)
		return
	}

	if lastErr != nil {
		http.Error(w, lastErr.Error(), 502)
	}
}

func resolveOpenAIChatCompletionsURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		base := strings.TrimRight(trimmed, "/")
		if strings.HasSuffix(base, "/chat/completions") {
			return base
		}
		if strings.HasSuffix(base, "/v1") {
			return base + "/chat/completions"
		}
		return base + "/v1/chat/completions"
	}

	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/chat/completions") {
		parsed.Path = path
		return parsed.String()
	}
	if path == "" {
		parsed.Path = "/v1/chat/completions"
	} else {
		parsed.Path = path + "/chat/completions"
	}
	return parsed.String()
}

// forwardAnthropic translates and forwards to Anthropic Messages API.
func forwardAnthropic(w http.ResponseWriter, incoming *http.Request, m *storage.CustomModel, geminiReq map[string]any, requestID string) {
	anthReq := toAnthropicRequest(geminiReq, m.ExternalModelName)
	if budget := reasoningBudget(m.ReasoningEffort); budget > 0 {
		anthReq["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		delete(anthReq, "temperature")
		if maxTokens, ok := numberAsInt(anthReq["max_tokens"]); !ok || maxTokens <= budget {
			anthReq["max_tokens"] = budget + 8192
		}
	}
	breakpointCount := applyAnthropicPromptCaching(anthReq)
	cacheEnabled := true

	apiURL := resolveAnthropicMessagesURL(m.APIURL)

	client := &http.Client{Timeout: 5 * time.Minute}
	var lastErr error
	extraAttempts := 0
	for attempt := 1; attempt <= maxRetries+extraAttempts; attempt++ {
		body, _ := json.Marshal(anthReq)
		trace("anthropic-upstream-request", map[string]any{
			"requestId": requestID, "attempt": attempt,
			"promptCache": cacheEnabled, "promptCacheBreakpoints": func() int {
				if cacheEnabled {
					return breakpointCount
				}
				return 0
			}(),
		})
		req, err := http.NewRequestWithContext(incoming.Context(), "POST", apiURL, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", m.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			trace("anthropic-upstream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			if attempt < maxRetries+extraAttempts && incoming.Context().Err() == nil {
				delay := retryDelay(attempt, "")
				trace("anthropic-upstream-retry", map[string]any{"requestId": requestID, "attempt": attempt, "delayMs": delay.Milliseconds()})
				time.Sleep(delay)
				continue
			}
			break
		}
		lastErr = nil

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			trace("anthropic-upstream-error-response", map[string]any{
				"requestId": requestID, "statusCode": resp.StatusCode,
				"body": string(errBody[:min(len(errBody), 500)]),
			})
			if cacheEnabled && isUnsupportedCacheResponse(resp.StatusCode, string(errBody)) {
				stripAnthropicPromptCaching(anthReq)
				cacheEnabled = false
				extraAttempts = 1
				trace("prompt-cache-fallback", map[string]any{
					"requestId": requestID, "provider": "anthropic", "statusCode": resp.StatusCode,
				})
				continue
			}
			if isRetryableStatus(resp.StatusCode) && attempt < maxRetries+extraAttempts {
				delay := retryDelay(attempt, resp.Header.Get("Retry-After"))
				trace("anthropic-upstream-retry", map[string]any{"requestId": requestID, "attempt": attempt, "statusCode": resp.StatusCode, "delayMs": delay.Milliseconds()})
				time.Sleep(delay)
				continue
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			w.Write(errBody)
			return
		}

		defer resp.Body.Close()
		streamAnthropicResponse(w, resp, requestID, attempt)
		return
	}
	if lastErr != nil {
		http.Error(w, lastErr.Error(), 502)
	}
}

func resolveAnthropicMessagesURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		base := strings.TrimRight(trimmed, "/")
		if strings.HasSuffix(base, "/messages") {
			return base
		}
		if strings.HasSuffix(base, "/chat/completions") {
			return strings.TrimSuffix(base, "/chat/completions") + "/messages"
		}
		if strings.HasSuffix(base, "/v1") {
			return base + "/messages"
		}
		return base + "/v1/messages"
	}

	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/messages"):
		parsed.Path = path
	case strings.HasSuffix(path, "/chat/completions"):
		parsed.Path = strings.TrimSuffix(path, "/chat/completions") + "/messages"
	case path == "":
		parsed.Path = "/v1/messages"
	case strings.HasSuffix(path, "/v1"):
		parsed.Path = path + "/messages"
	default:
		parsed.Path = path + "/v1/messages"
	}
	return parsed.String()
}

func reasoningBudget(effort string) int {
	switch effort {
	case "low":
		return 1024
	case "medium":
		return 4096
	case "high":
		return 8192
	default:
		return 0
	}
}

func numberAsInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

// streamOpenAIResponse streams an OpenAI SSE response converting to Gemini format.
func streamOpenAIResponse(w http.ResponseWriter, resp *http.Response, requestID string, attempt int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	streamState := openAIStreamState{traceID: requestID}
	startedAt := time.Now()
	var firstByteAt time.Time
	wroteEvent := false

	for scanner.Scan() {
		line := scanner.Text()
		if firstByteAt.IsZero() && len(line) > 0 {
			firstByteAt = time.Now()
		}
		geminiLine := convertOpenAILineToGemini(line, &streamState)
		if geminiLine != "" {
			if !wroteEvent {
				w.WriteHeader(http.StatusOK)
				wroteEvent = true
			}
			w.Write([]byte(geminiLine))
			if canFlush {
				flusher.Flush()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if !wroteEvent {
			writeEmptyUpstreamStreamError(w, "openai", requestID, attempt, resp.Header.Get("Content-Type"), err.Error())
			return
		}
		trace("openai-stream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error(), "downstreamCommitted": true})
	}
	if !wroteEvent {
		writeEmptyUpstreamStreamError(w, "openai", requestID, attempt, resp.Header.Get("Content-Type"), "上游响应中没有可识别的 OpenAI SSE 事件")
		return
	}
	if !streamState.finished {
		trace("openai-stream-missing-stop", map[string]any{"requestId": requestID, "attempt": attempt})
		w.Write([]byte(encodeAntigravityStreamEvent(finalStopResponse(streamState.modelVersion, streamState.responseID), requestID)))
		if canFlush {
			flusher.Flush()
		}
	}

	if streamState.usage != nil {
		promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens := openAIUsage(streamState.usage)
		trace("usage", map[string]any{
			"requestId":        requestID,
			"promptTokens":     promptTokens,
			"completionTokens": completionTokens,
			"cacheReadTokens":  cacheReadTokens,
			"cacheWriteTokens": cacheWriteTokens,
			"firstByteMs":      firstByteAt.Sub(startedAt).Milliseconds(),
			"totalMs":          time.Since(startedAt).Milliseconds(),
		})
	}
}

func openAIUsage(usage map[string]any) (prompt, completion, cacheRead, cacheWrite int) {
	prompt, _ = numberAsInt(usage["prompt_tokens"])
	completion, _ = numberAsInt(usage["completion_tokens"])
	if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		cacheRead, _ = numberAsInt(details["cached_tokens"])
		if value, ok := numberAsInt(details["cache_write_tokens"]); ok {
			cacheWrite = value
		}
	}
	if value, ok := numberAsInt(usage["cache_read_input_tokens"]); ok {
		cacheRead = value
	}
	if value, ok := numberAsInt(usage["cache_creation_input_tokens"]); ok {
		cacheWrite = value
	}
	return
}

// streamAnthropicResponse streams an Anthropic SSE response converting to Gemini format.
func streamAnthropicResponse(w http.ResponseWriter, resp *http.Response, requestID string, attempt int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	startedAt := time.Now()
	var firstByteAt time.Time
	totals := anthropicUsageTotals{}
	state := anthropicStreamState{traceID: requestID}
	wroteEvent := false

	for scanner.Scan() {
		line := scanner.Text()
		if firstByteAt.IsZero() && len(line) > 0 {
			firstByteAt = time.Now()
		}
		collectAnthropicUsage(line, &totals)
		geminiLine := convertAnthropicLineToGemini(line, &state)
		if geminiLine != "" {
			if !wroteEvent {
				w.WriteHeader(http.StatusOK)
				wroteEvent = true
			}
			w.Write([]byte(geminiLine))
			if canFlush {
				flusher.Flush()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if !wroteEvent {
			writeEmptyUpstreamStreamError(w, "anthropic", requestID, attempt, resp.Header.Get("Content-Type"), err.Error())
			return
		}
		trace("anthropic-stream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error(), "downstreamCommitted": true})
	}
	if !wroteEvent {
		writeEmptyUpstreamStreamError(w, "anthropic", requestID, attempt, resp.Header.Get("Content-Type"), "上游响应中没有可识别的 Anthropic SSE 事件")
		return
	}
	if !state.finished {
		trace("anthropic-stream-missing-stop", map[string]any{"requestId": requestID, "attempt": attempt})
		w.Write([]byte(encodeAntigravityStreamEvent(finalStopResponse(state.modelVersion, state.responseID), requestID)))
		if canFlush {
			flusher.Flush()
		}
	}

	if totals.seen {
		trace("usage", map[string]any{
			"requestId":        requestID,
			"promptTokens":     totals.input + totals.cacheRead + totals.cacheWrite,
			"completionTokens": totals.output,
			"cacheReadTokens":  totals.cacheRead,
			"cacheWriteTokens": totals.cacheWrite,
			"firstByteMs":      firstByteAt.Sub(startedAt).Milliseconds(),
			"totalMs":          time.Since(startedAt).Milliseconds(),
		})
	}
}

func finalStopResponse(modelVersion, responseID string) map[string]any {
	response := map[string]any{
		"candidates": []any{map[string]any{
			"content":      map[string]any{"role": "model", "parts": []any{}},
			"finishReason": "STOP",
		}},
	}
	if modelVersion != "" {
		response["modelVersion"] = modelVersion
	}
	if responseID != "" {
		response["responseId"] = responseID
	}
	return response
}

func writeEmptyUpstreamStreamError(w http.ResponseWriter, provider, requestID string, attempt int, contentType, message string) {
	trace(provider+"-empty-stream", map[string]any{
		"requestId":   requestID,
		"attempt":     attempt,
		"contentType": contentType,
		"message":     message,
	})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Del("Content-Disposition")
	w.Header().Del("Connection")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_upstream_stream",
		},
	})
}

func isRetryableStatus(code int) bool {
	return code == 429 || code == 502 || code == 503 || code == 504 || code == 524
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if seconds, err := strconv.ParseFloat(retryAfter, 64); err == nil && seconds >= 0 {
		return minDuration(time.Duration(seconds*float64(time.Second)), 10*time.Second)
	}
	if date, err := http.ParseTime(retryAfter); err == nil {
		return minDuration(maxDuration(time.Until(date), 0), 10*time.Second)
	}
	delay := 250 * time.Millisecond * time.Duration(1<<(attempt-1))
	return minDuration(delay, 2*time.Second)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
