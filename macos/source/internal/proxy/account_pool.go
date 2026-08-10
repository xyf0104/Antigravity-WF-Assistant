package proxy

import (
	"fmt"
	"net/http"
	"strings"

	"antigravity-wf-assistant/internal/storage"
)

// acquireAttemptModel selects an account for one upstream attempt. The model
// payload remains model-specific, while URL, headers and credentials come from
// the selected account. Excluded IDs prevent a retry from immediately sending
// the same failed request to the same depleted account. The selected account is
// only excluded after a genuine upstream failure: a prompt-cache compatibility
// retry must be allowed to use the same healthy account.
func acquireAttemptModel(base *storage.CustomModel, excluded map[string]struct{}) (*storage.CustomModel, *storage.AccountLease, error) {
	selected, lease, err := storage.AcquireAccountForModel(*base, excluded)
	if err != nil {
		return nil, nil, err
	}
	return &selected, lease, nil
}

func excludeFailedAttempt(excluded map[string]struct{}, lease *storage.AccountLease) {
	if lease != nil && lease.ID != "" {
		excluded[lease.ID] = struct{}{}
	}
}

// observeAttemptQuota passively records rate-limit headers for the account
// selected for this upstream attempt. It deliberately runs before any retry,
// fallback, or response-body handling so both accepted and rejected upstream
// responses remain visible in the account view.
func observeAttemptQuota(lease *storage.AccountLease, provider string, response *http.Response) {
	if lease == nil || lease.ID == "" || response == nil {
		return
	}
	storage.ObserveUpstreamQuota(lease.ID, provider, response.StatusCode, response.Header)
}

func releaseAttemptSuccess(lease *storage.AccountLease) {
	if lease != nil {
		lease.Finish(http.StatusOK, "", "")
	}
}

func releaseAttemptFailure(lease *storage.AccountLease, statusCode int, retryAfter, message string) {
	if lease == nil {
		return
	}
	if shouldCooldownAccount(statusCode) {
		lease.Finish(statusCode, retryAfter, message)
		return
	}
	// A regular 4xx is usually a model/request validation issue. The account
	// itself remains usable and should not be penalised by the scheduler.
	lease.Finish(http.StatusOK, "", "")
}

func shouldCooldownAccount(statusCode int) bool {
	return statusCode == 0 || statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden ||
		statusCode == http.StatusTooManyRequests || statusCode == http.StatusBadGateway ||
		statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout || statusCode == 524
}

// shouldFailOverAccount adds credential failures to the normal transient
// retry list, but only when a model is actually bound to an account pool. A
// legacy per-model API key still receives the upstream response directly.
func shouldFailOverAccount(lease *storage.AccountLease, statusCode int) bool {
	if lease == nil {
		return isRetryableStatus(statusCode)
	}
	return statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || isRetryableStatus(statusCode)
}

func accountPoolError(provider string, err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "账户池没有可用账户"
	}
	return fmt.Sprintf("%s 账户池不可用：%s", provider, message)
}
