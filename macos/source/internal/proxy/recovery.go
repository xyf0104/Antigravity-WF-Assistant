package proxy

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"antigravity-byok/internal/storage"
)

const upstreamStreamTimeout = 15 * time.Minute

// streamRecoveryPolicy is kept in memory so a model request never needs to
// read the settings file on its hot path. The application updates it whenever
// the user saves the Settings page.
type streamRecoveryPolicy struct {
	enabled         bool
	maxAttempts     int
	maxDelaySeconds int
}

var streamRecoveryConfig = struct {
	sync.RWMutex
	policy streamRecoveryPolicy
}{
	policy: streamRecoveryPolicy{enabled: true, maxAttempts: 5, maxDelaySeconds: 20},
}

func ConfigureStreamRecovery(settings storage.StreamRecoverySettings) {
	defaults := storage.DefaultAppSettings().StreamRecovery
	if settings.MaxAttempts <= 0 {
		settings.MaxAttempts = defaults.MaxAttempts
	}
	if settings.MaxAttempts > 10 {
		settings.MaxAttempts = 10
	}
	if settings.MaxDelaySeconds <= 0 {
		settings.MaxDelaySeconds = defaults.MaxDelaySeconds
	}
	if settings.MaxDelaySeconds > 120 {
		settings.MaxDelaySeconds = 120
	}
	streamRecoveryConfig.Lock()
	streamRecoveryConfig.policy = streamRecoveryPolicy{
		enabled: settings.Enabled, maxAttempts: settings.MaxAttempts, maxDelaySeconds: settings.MaxDelaySeconds,
	}
	streamRecoveryConfig.Unlock()
}

func currentStreamRecoveryPolicy() streamRecoveryPolicy {
	streamRecoveryConfig.RLock()
	defer streamRecoveryConfig.RUnlock()
	return streamRecoveryConfig.policy
}

// downstreamSSEWriter tracks whether the HTTP response was committed. This is
// essential for recovery: once Antigravity has received part of an answer, an
// upstream HTTP error can no longer be forwarded as a second HTTP response.
type downstreamSSEWriter struct {
	w         http.ResponseWriter
	flusher   http.Flusher
	committed bool
}

func newDownstreamSSEWriter(w http.ResponseWriter) *downstreamSSEWriter {
	flusher, _ := w.(http.Flusher)
	return &downstreamSSEWriter{w: w, flusher: flusher}
}

func (writer *downstreamSSEWriter) prepare() {
	writer.w.Header().Set("Content-Type", "text/event-stream")
	writer.w.Header().Set("Content-Disposition", "attachment")
	writer.w.Header().Set("Cache-Control", "no-cache")
	writer.w.Header().Set("Connection", "keep-alive")
}

func (writer *downstreamSSEWriter) write(event string) {
	if event == "" {
		return
	}
	writer.prepare()
	if !writer.committed {
		writer.w.WriteHeader(http.StatusOK)
		writer.committed = true
	}
	_, _ = writer.w.Write([]byte(event))
	if writer.flusher != nil {
		writer.flusher.Flush()
	}
}

// writeComment is a valid SSE keepalive that Antigravity ignores as content.
// It prevents a local client-side idle timeout while an upstream account pool
// is rotating or a temporary network error is being retried.
func (writer *downstreamSSEWriter) writeComment(comment string) {
	comment = strings.ReplaceAll(strings.ReplaceAll(comment, "\r", " "), "\n", " ")
	writer.write(": " + comment + "\n\n")
}

func recoveryDelay(policy streamRecoveryPolicy, attempt int, retryAfter string) time.Duration {
	delay := retryDelay(attempt, retryAfter)
	maximum := time.Duration(policy.maxDelaySeconds) * time.Second
	if maximum > 0 && delay > maximum {
		return maximum
	}
	return delay
}

// waitForStreamRecovery keeps the downstream SSE connection alive while
// waiting for a retry. It returns false if the caller disconnected or the
// configured retry budget has been exhausted.
func waitForStreamRecovery(ctx context.Context, writer *downstreamSSEWriter, policy streamRecoveryPolicy, provider, requestID, reason, retryAfter string, reconnect int) bool {
	if !policy.enabled || reconnect <= 0 || reconnect > policy.maxAttempts || ctx.Err() != nil {
		return false
	}
	delay := recoveryDelay(policy, reconnect, retryAfter)
	trace(provider+"-stream-reconnect", map[string]any{
		"requestId": requestID, "reconnect": reconnect, "maxReconnects": policy.maxAttempts,
		"delayMs": delay.Milliseconds(), "reason": reason,
	})
	writer.writeComment(fmt.Sprintf("wf-reconnecting %d/%d", reconnect, policy.maxAttempts))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func writeRecoveredStreamStop(writer *downstreamSSEWriter, requestID, modelVersion, responseID string, reconnects int, unsafe bool) {
	message := fmt.Sprintf("上游连接已自动重连 %d 次仍未恢复，已保留当前回复。请直接继续提问以从现有上下文继续。", reconnects)
	if unsafe {
		message = "上游在工具调用或生成附件期间中断。为避免重复执行操作，已保留当前回复并安全结束；请直接继续提问以恢复。"
	}
	writer.write(encodeAntigravityStreamEvent(map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": message}}},
		}},
	}, requestID))
	writer.write(encodeAntigravityStreamEvent(finalStopResponse(modelVersion, responseID), requestID))
}

// continuationExcerpt is deliberately bounded. The original prompt is already
// sent with every retry; only the tail of emitted text is required to tell an
// account selected after a failover exactly where to continue.
func continuationExcerpt(text string) string {
	const maxRunes = 12000
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return "[…前文已在当前会话中保留…]" + string(runes[len(runes)-maxRunes:])
}

func continuationInstruction() string {
	return "上游流式连接刚刚中断。上方助手内容已经展示给用户；请从其最后一个字符后继续回答，不要重复已有内容，不要重复调用已经完成的工具，只输出后续内容。"
}

func shallowCloneRequest(request map[string]any) map[string]any {
	clone := make(map[string]any, len(request)+1)
	for key, value := range request {
		clone[key] = value
	}
	return clone
}

func appendRequestItems(value any, additions ...any) []any {
	items := make([]any, 0, len(additions)+4)
	switch current := value.(type) {
	case []any:
		items = append(items, current...)
	case []map[string]any:
		for _, item := range current {
			items = append(items, item)
		}
	case nil:
		// Keep an empty list when the original request has no conversation.
	}
	items = append(items, additions...)
	return items
}

func continueOpenAIChatRequest(base map[string]any, emittedText string) map[string]any {
	request := shallowCloneRequest(base)
	partial := continuationExcerpt(emittedText)
	if partial == "" {
		return request
	}
	request["messages"] = appendRequestItems(request["messages"],
		map[string]any{"role": "assistant", "content": partial},
		map[string]any{"role": "user", "content": continuationInstruction()},
	)
	return request
}

func continueAnthropicRequest(base map[string]any, emittedText string) map[string]any {
	request := shallowCloneRequest(base)
	partial := continuationExcerpt(emittedText)
	if partial == "" {
		return request
	}
	request["messages"] = appendRequestItems(request["messages"],
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": partial}}},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": continuationInstruction()}}},
	)
	return request
}

func continueResponsesRequest(base map[string]any, emittedText string) map[string]any {
	request := shallowCloneRequest(base)
	partial := continuationExcerpt(emittedText)
	if partial == "" {
		return request
	}
	request["input"] = appendRequestItems(request["input"],
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": partial}}},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": continuationInstruction()}}},
	)
	return request
}

type streamAttemptOutcome struct {
	wroteEvent   bool
	finished     bool
	emittedText  string
	unsafeOutput bool
	responseID   string
	modelVersion string
	err          error
}

func (outcome streamAttemptOutcome) canContinue() bool {
	return !outcome.unsafeOutput && strings.TrimSpace(outcome.emittedText) != ""
}
