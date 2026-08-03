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
	policy: streamRecoveryPolicy{enabled: true, maxAttempts: 2, maxDelaySeconds: 20},
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
	// Do not append a synthetic assistant message here. It would be persisted in
	// Antigravity's conversation and then sent back to the upstream on the next
	// user turn, which is both confusing and a source of repeated replies.
	// A valid SSE comment records the state without becoming chat content.
	reason := "partial-output"
	if unsafe {
		reason = "tool-or-attachment"
	}
	writer.writeComment(fmt.Sprintf("wf-stream-stopped reconnects=%d reason=%s", reconnects, reason))
	writer.write(encodeAntigravityStreamEvent(finalStopResponse(modelVersion, responseID), requestID))
}

// canRetryStreamWithoutContinuation is intentionally stricter than a normal
// network retry. Once an upstream event has reached Antigravity, retrying the
// generation can execute the same request twice even when there is no visible
// network outage. We therefore retry only while the request has produced no
// downstream event at all. This keeps an accepted generation single-shot and
// prevents duplicated chat turns, tool calls, and token charges.
func canRetryStreamWithoutContinuation(emittedText string, outputDelivered, unsafeOutput bool) bool {
	return !outputDelivered && !unsafeOutput && strings.TrimSpace(emittedText) == ""
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
