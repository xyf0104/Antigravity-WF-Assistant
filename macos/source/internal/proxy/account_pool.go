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
	if shouldCooldownAccount(statusCode, message) {
		lease.Finish(statusCode, retryAfter, message)
		return
	}
	// A regular 4xx is usually a model/request validation issue. The account
	// itself remains usable and should not be penalised by the scheduler.
	lease.Finish(http.StatusOK, "", "")
}

func shouldCooldownAccount(statusCode int, message string) bool {
	return statusCode == 0 || statusCode == http.StatusUnauthorized ||
		(statusCode == http.StatusForbidden && isAccountLevelForbidden(message)) ||
		statusCode == http.StatusTooManyRequests || statusCode == http.StatusBadGateway ||
		statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout || statusCode == 524
}

// shouldFailOverAccount adds credential failures to the normal transient
// retry list, but only when a model is actually bound to an account pool. A
// legacy per-model API key still receives the upstream response directly.
func shouldFailOverAccount(lease *storage.AccountLease, statusCode int, body string) bool {
	if lease == nil {
		return isRetryableStatus(statusCode) || isTransientProviderRejection(statusCode, body)
	}
	if statusCode == http.StatusNotFound && isModelRouteRejection(body) {
		return lease.HasAlternatives
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return lease.HasAlternatives || isTransientProviderRejection(statusCode, body)
	}
	return isRetryableStatus(statusCode)
}

// shouldRetrySameAccount is intentionally narrower than failover. A concrete
// 429/5xx or provider-route rejection proves generation did not start and may
// recover on a second gateway attempt. Authentication, balance and model-name
// errors cannot improve by immediately replaying the same account.
func shouldRetrySameAccount(lease *storage.AccountLease, statusCode int, body string) bool {
	if lease != nil && lease.HasAlternatives {
		return false
	}
	return isRetryableStatus(statusCode) || isTransientProviderRejection(statusCode, body)
}

func isAccountLevelForbidden(body string) bool {
	value := strings.ToLower(body)
	for _, marker := range []string{
		"insufficient balance", "insufficient_balance", "billing_error", "payment required",
		"quota exhausted", "quota_exhausted", "credit balance", "invalid api key",
		"invalid_api_key", "authentication", "access token", "api key expired",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func isTransientProviderRejection(statusCode int, body string) bool {
	if statusCode != http.StatusForbidden || isAccountLevelForbidden(body) {
		return false
	}
	value := strings.ToLower(body)
	for _, marker := range []string{
		"provider terms of service", "provider terms", "provider unavailable",
		"provider_name", "permission_denied", "permission_error",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func isModelRouteRejection(body string) bool {
	value := strings.ToLower(body)
	for _, marker := range []string{
		"model_not_found", "model not found", "model is not supported",
		"not supported by any configured account", "no available channel for model",
		"no provider available for model",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
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
