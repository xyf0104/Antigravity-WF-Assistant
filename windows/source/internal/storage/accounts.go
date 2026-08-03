package storage

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UpstreamAccount is a reusable credential and scheduling unit. Models can
// bind one or more account IDs; the local proxy then selects an eligible
// account without exposing account rotation to Antigravity.
//
// Credentials intentionally accepts imported JSON from common XIASS-style
// exports. APIKey remains the canonical value for a manually-created account;
// EffectiveAPIKey reads both forms so imports do not need a lossy conversion.
type UpstreamAccount struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Notes           string            `json:"notes,omitempty"`
	Provider        string            `json:"provider"`
	Type            string            `json:"type"`
	APIURL          string            `json:"apiUrl"`
	EndpointMode    string            `json:"endpointMode,omitempty"`
	APIStyle        string            `json:"apiStyle,omitempty"`
	MessagePathMode string            `json:"messagePathMode,omitempty"`
	AuthMode        string            `json:"authMode,omitempty"`
	AuthHeader      string            `json:"authHeader,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	// QuotaURL is an optional, provider-documented full endpoint queried only
	// when the user explicitly requests an upstream quota refresh.
	QuotaURL string `json:"quotaUrl,omitempty"`
	// OAuth contains only public OAuth client configuration. Access and refresh
	// tokens remain inside Credentials and are never returned to the renderer.
	OAuth       OAuthConfiguration `json:"oauth,omitempty"`
	APIKey      string             `json:"apiKey,omitempty"`
	Credentials map[string]any     `json:"credentials,omitempty"`
	// Identity is display-only account metadata. Its Source makes clear whether
	// it came from an OAuth response, an imported JSON file, or has not yet
	// been verified by an upstream provider.
	Identity AccountIdentity `json:"identity,omitempty"`
	// AuthExpiresAt describes the access-token lifetime. It is deliberately
	// separate from ExpiresAt, which means the local scheduler must stop using
	// an account at a user-configured time.
	AuthExpiresAt  string        `json:"authExpiresAt,omitempty"`
	Quota          QuotaSnapshot `json:"quota,omitempty"`
	Enabled        bool          `json:"enabled"`
	Priority       int           `json:"priority"`
	MaxConcurrency int           `json:"maxConcurrency"`
	ExpiresAt      string        `json:"expiresAt,omitempty"`
	LastUsedAt     string        `json:"lastUsedAt,omitempty"`
	LastSuccessAt  string        `json:"lastSuccessAt,omitempty"`
	LastError      string        `json:"lastError,omitempty"`
	FailureCount   int           `json:"failureCount,omitempty"`
	CooldownUntil  string        `json:"cooldownUntil,omitempty"`
	CreatedAt      string        `json:"createdAt,omitempty"`
	UpdatedAt      string        `json:"updatedAt,omitempty"`
	ActiveRequests int           `json:"activeRequests,omitempty"`
}

// OAuthConfiguration is a public-client Authorization Code + PKCE setup.
// WF intentionally does not store a client secret: a desktop app is not a
// confidential OAuth client. The provider must have registered the redirect
// URI and client ID entered by the account owner.
type OAuthConfiguration struct {
	AuthorizationURL string `json:"authorizationUrl,omitempty"`
	TokenURL         string `json:"tokenUrl,omitempty"`
	ClientID         string `json:"clientId,omitempty"`
	RedirectURI      string `json:"redirectUri,omitempty"`
	Scopes           string `json:"scopes,omitempty"`
}

// AccountIdentity is metadata shown beside a credential. It is never used to
// authorize requests or infer billing entitlement.
type AccountIdentity struct {
	Email                 string `json:"email,omitempty"`
	Subject               string `json:"subject,omitempty"`
	Plan                  string `json:"plan,omitempty"`
	OrganizationID        string `json:"organizationId,omitempty"`
	SubscriptionExpiresAt string `json:"subscriptionExpiresAt,omitempty"`
	PrivacyMode           string `json:"privacyMode,omitempty"`
	Source                string `json:"source,omitempty"`
	UpdatedAt             string `json:"updatedAt,omitempty"`
}

// QuotaSnapshot only contains values directly observed in upstream response
// headers or a user-requested quota endpoint. Empty fields mean the upstream
// did not expose that value; callers must not turn absence into a zero quota.
type QuotaSnapshot struct {
	Available         bool   `json:"available,omitempty"`
	Source            string `json:"source,omitempty"`
	UpdatedAt         string `json:"updatedAt,omitempty"`
	StatusCode        int    `json:"statusCode,omitempty"`
	RequestsRemaining string `json:"requestsRemaining,omitempty"`
	TokensRemaining   string `json:"tokensRemaining,omitempty"`
	RequestsReset     string `json:"requestsReset,omitempty"`
	TokensReset       string `json:"tokensReset,omitempty"`
	RetryAfter        string `json:"retryAfter,omitempty"`
	Message           string `json:"message,omitempty"`
}

type accountsStore struct {
	Accounts []UpstreamAccount `json:"accounts"`
}

type AccountImportResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Added   int    `json:"added"`
}

// AccountLease represents one in-flight request reservation. Finish must be
// called once the upstream response is complete or failed so concurrency and
// cooldown state remain accurate.
type AccountLease struct {
	ID   string
	once sync.Once
}

var (
	accountsMu   sync.RWMutex
	accountsFile string
	accountRun   = struct {
		sync.Mutex
		active map[string]int
	}{active: make(map[string]int)}
	quotaRun = struct {
		sync.RWMutex
		values        map[string]QuotaSnapshot
		lastPersisted map[string]time.Time
	}{values: make(map[string]QuotaSnapshot), lastPersisted: make(map[string]time.Time)}
)

func initAccountsFile(dir string) {
	accountsFile = filepath.Join(dir, "upstream_accounts.json")
	_ = os.Chmod(accountsFile, 0o600)
}

func normalizeAccount(account UpstreamAccount) UpstreamAccount {
	wasNew := strings.TrimSpace(account.ID) == ""
	if wasNew {
		account.ID = newAccountID()
	}
	account.Name = strings.TrimSpace(account.Name)
	account.Notes = strings.TrimSpace(account.Notes)
	account.Provider = normalizeAccountProvider(account.Provider)
	account.Type = normalizeAccountType(account.Type)
	// Do not rewrite a manually-entered full endpoint. In particular, a query
	// string or trailing slash may be meaningful to a gateway owned by the user.
	account.APIURL = strings.TrimSpace(account.APIURL)
	if account.APIURL == "" {
		account.APIURL = "https://api.xiass.com"
	}
	account.APIStyle = strings.ToLower(strings.TrimSpace(account.APIStyle))
	account.EndpointMode = normalizeEndpointMode(account.EndpointMode)
	account.MessagePathMode = normalizeMessagePathMode(account.MessagePathMode)
	account.AuthMode = strings.ToLower(strings.TrimSpace(account.AuthMode))
	account.AuthHeader = strings.TrimSpace(account.AuthHeader)
	account.QuotaURL = strings.TrimSpace(account.QuotaURL)
	account.OAuth = normalizeOAuthConfiguration(account.OAuth)
	account.Identity = normalizeAccountIdentity(account.Identity)
	account.AuthExpiresAt = strings.TrimSpace(account.AuthExpiresAt)
	account.Quota = normalizeQuotaSnapshot(account.Quota)
	if account.AuthMode == "" {
		switch account.Type {
		case "x_api_key":
			account.AuthMode = "x_api_key"
		case "custom_header":
			account.AuthMode = "custom_header"
		case "api_key":
			if account.Provider == "anthropic" {
				account.AuthMode = "x_api_key"
			} else {
				account.AuthMode = "bearer"
			}
		default:
			account.AuthMode = "bearer"
		}
	}
	if account.MaxConcurrency <= 0 {
		account.MaxConcurrency = 2
	}
	if account.MaxConcurrency > 32 {
		account.MaxConcurrency = 32
	}
	if account.Priority < 0 {
		account.Priority = 0
	}
	if account.Priority > 1000 {
		account.Priority = 1000
	}
	if wasNew {
		account.Enabled = true
	}
	if account.Name == "" {
		account.Name = accountProviderLabel(account.Provider) + " 账户"
	}
	if account.Headers != nil {
		account.Headers = cloneStringMap(account.Headers)
	}
	if account.Credentials != nil {
		account.Credentials = normalizeImportedCredentials(account.Credentials)
	}
	populateIdentityFromCredentials(&account, "导入的 OAuth 声明")
	now := time.Now().UTC().Format(time.RFC3339)
	if account.CreatedAt == "" {
		account.CreatedAt = now
	}
	account.UpdatedAt = now
	return account
}

func normalizeAccountProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "anthropic", "claude":
		return "anthropic"
	case "grok", "xai", "x.ai":
		return "grok"
	case "gemini", "google", "antigravity":
		// Gemini and Antigravity credentials are normally consumed through an
		// OpenAI-compatible XIASS endpoint, so retain their identity for UI and
		// scheduling but use the compatible request bridge in the proxy.
		return "openai"
	case "custom", "compatible", "openai":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "openai"
	}
}

func accountProviderLabel(provider string) string {
	switch normalizeAccountProvider(provider) {
	case "anthropic":
		return "Claude"
	case "grok":
		return "Grok"
	case "custom":
		return "兼容接口"
	default:
		return "OpenAI"
	}
}

func normalizeAccountType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "api_key", "apikey", "api-key":
		return "api_key"
	case "x_api_key", "x-api-key":
		return "x_api_key"
	case "oauth", "oauth_token", "access_token", "bearer", "bearer_token":
		return "oauth"
	case "auth_json", "auth.json", "auth-json", "oauth_json", "oauth-json", "credential_json", "credential-json", "credentials_json", "credentials-json":
		return "auth_json"
	case "refresh_token", "refresh-token", "refreshtoken", "mobile_rt", "mobile-rt", "mobilert":
		return "refresh_token"
	case "codex_pat", "pat", "personal_access_token", "personal-access-token":
		return "codex_pat"
	case "setup_token", "setup-token":
		return "setup_token"
	case "custom_header", "custom-header":
		return "custom_header"
	case "json", "json_import", "json-import", "raw_json", "raw-json", "account_json", "account-json", "import":
		return "json"
	default:
		return "api_key"
	}
}

// compactAccountType deliberately treats punctuation and case variants as the
// same account type. The UI is not the only caller of this package, so the
// storage boundary must not rely on a renderer having normalized the value.
func compactAccountType(value string) string {
	var compact strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			compact.WriteRune(character)
		}
	}
	return compact.String()
}

// isRawJSONAccountType identifies an instruction to import credentials, not a
// usable authentication scheme. Raw JSON must be parsed by
// ImportUpstreamAccounts so that only the intended credential fields are kept.
func isRawJSONAccountType(value string) bool {
	switch compactAccountType(value) {
	case "authjson", "oauthjson", "credentialjson", "credentialsjson", "json", "jsonimport", "rawjson", "accountjson", "import":
		return true
	default:
		return false
	}
}

// isRefreshTokenAccountType prevents a refresh token (including Mobile RT)
// from being stored as a bearer/API key. It has to be exchanged first, which
// keeps the account's token lifetime and refresh metadata consistent.
func isRefreshTokenAccountType(value string) bool {
	switch compactAccountType(value) {
	case "refreshtoken", "mobilert", "mobilerefreshtoken":
		return true
	default:
		return false
	}
}

func normalizeMessagePathMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "standard", "compat", "manual":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "auto"
	}
}

func normalizeEndpointMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "manual", "exact", "full", "full_url", "full-url":
		return "manual"
	default:
		return "auto"
	}
}

func newAccountID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("acct-%d", time.Now().UnixNano())
	}
	return "acct-" + hex.EncodeToString(buf)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	var cloned map[string]any
	if json.Unmarshal(data, &cloned) != nil {
		return nil
	}
	return cloned
}

// EffectiveAPIKey extracts the usable secret without leaking it to logs.
func (account UpstreamAccount) EffectiveAPIKey() string {
	if value := strings.TrimSpace(account.APIKey); value != "" {
		return value
	}
	return effectiveAPIKeyFromCredentials(account.Credentials)
}

func (account UpstreamAccount) ToModel(model CustomModel) CustomModel {
	model.Provider = account.Provider
	model.APIURL = account.APIURL
	model.APIKey = account.EffectiveAPIKey()
	if account.APIStyle != "" {
		model.APIStyle = account.APIStyle
	}
	if account.EndpointMode != "" {
		model.EndpointMode = account.EndpointMode
	}
	if account.MessagePathMode != "" {
		model.MessagePathMode = account.MessagePathMode
	}
	model.AuthMode = account.AuthMode
	model.AuthHeader = account.AuthHeader
	model.Headers = cloneStringMap(account.Headers)
	return model
}

func LoadUpstreamAccounts() ([]UpstreamAccount, error) {
	accountsMu.RLock()
	defer accountsMu.RUnlock()
	accounts, err := loadAccountsLocked()
	if err != nil {
		return nil, err
	}
	return withActiveRequestCounts(accounts), nil
}

func loadAccountsLocked() ([]UpstreamAccount, error) {
	if accountsFile == "" {
		return nil, nil
	}
	data, err := os.ReadFile(accountsFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var store accountsStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	for i := range store.Accounts {
		store.Accounts[i] = normalizeLoadedAccount(store.Accounts[i])
	}
	return store.Accounts, nil
}

func normalizeLoadedAccount(account UpstreamAccount) UpstreamAccount {
	account.Name = strings.TrimSpace(account.Name)
	account.Provider = normalizeAccountProvider(account.Provider)
	account.Type = normalizeAccountType(account.Type)
	account.APIURL = strings.TrimSpace(account.APIURL)
	if account.APIURL == "" {
		account.APIURL = "https://api.xiass.com"
	}
	account.MessagePathMode = normalizeMessagePathMode(account.MessagePathMode)
	account.EndpointMode = normalizeEndpointMode(account.EndpointMode)
	account.QuotaURL = strings.TrimSpace(account.QuotaURL)
	account.OAuth = normalizeOAuthConfiguration(account.OAuth)
	account.Identity = normalizeAccountIdentity(account.Identity)
	account.AuthExpiresAt = strings.TrimSpace(account.AuthExpiresAt)
	account.Quota = normalizeQuotaSnapshot(account.Quota)
	account.Credentials = normalizeImportedCredentials(account.Credentials)
	populateIdentityFromCredentials(&account, "导入的 OAuth 声明")
	if account.MaxConcurrency <= 0 {
		account.MaxConcurrency = 2
	}
	if account.MaxConcurrency > 32 {
		account.MaxConcurrency = 32
	}
	return account
}

func normalizeOAuthConfiguration(config OAuthConfiguration) OAuthConfiguration {
	config.AuthorizationURL = strings.TrimSpace(config.AuthorizationURL)
	config.TokenURL = strings.TrimSpace(config.TokenURL)
	config.ClientID = strings.TrimSpace(config.ClientID)
	config.RedirectURI = strings.TrimSpace(config.RedirectURI)
	config.Scopes = strings.Join(strings.Fields(config.Scopes), " ")
	return config
}

func normalizeAccountIdentity(identity AccountIdentity) AccountIdentity {
	identity.Email = strings.TrimSpace(identity.Email)
	identity.Subject = strings.TrimSpace(identity.Subject)
	identity.Plan = strings.TrimSpace(identity.Plan)
	identity.OrganizationID = strings.TrimSpace(identity.OrganizationID)
	identity.SubscriptionExpiresAt = strings.TrimSpace(identity.SubscriptionExpiresAt)
	identity.PrivacyMode = strings.TrimSpace(identity.PrivacyMode)
	identity.Source = strings.TrimSpace(identity.Source)
	identity.UpdatedAt = strings.TrimSpace(identity.UpdatedAt)
	return identity
}

func normalizeQuotaSnapshot(snapshot QuotaSnapshot) QuotaSnapshot {
	snapshot.Source = strings.TrimSpace(snapshot.Source)
	snapshot.UpdatedAt = strings.TrimSpace(snapshot.UpdatedAt)
	snapshot.RequestsRemaining = strings.TrimSpace(snapshot.RequestsRemaining)
	snapshot.TokensRemaining = strings.TrimSpace(snapshot.TokensRemaining)
	snapshot.RequestsReset = strings.TrimSpace(snapshot.RequestsReset)
	snapshot.TokensReset = strings.TrimSpace(snapshot.TokensReset)
	snapshot.RetryAfter = strings.TrimSpace(snapshot.RetryAfter)
	snapshot.Message = strings.TrimSpace(snapshot.Message)
	return snapshot
}

// populateIdentityFromCredentials imports non-secret OAuth metadata when it
// is available. JWT content is intentionally labelled as a claim because
// desktop credential import cannot prove the original token signature.
func populateIdentityFromCredentials(account *UpstreamAccount, fallbackSource string) {
	if account == nil {
		return
	}
	account.Credentials = normalizeImportedCredentials(account.Credentials)
	if account.Credentials == nil {
		return
	}
	credentialMaps := importedCredentialMaps(account.Credentials)
	identity := account.Identity
	if identity.Email == "" {
		identity.Email = importedStringFromMaps(credentialMaps, "email", "user_email", "userEmail")
	}
	if identity.Subject == "" {
		identity.Subject = importedStringFromMaps(credentialMaps, "sub", "subject", "user_id", "userId", "chatgpt_user_id")
	}
	if identity.Plan == "" {
		identity.Plan = importedStringFromMaps(credentialMaps, "plan", "plan_type", "planType", "chatgpt_plan_type", "tier")
	}
	if identity.OrganizationID == "" {
		identity.OrganizationID = importedStringFromMaps(credentialMaps, "organization_id", "organizationId", "org_id", "orgId", "account_id", "accountId", "chatgpt_account_id", "poid")
	}
	if identity.SubscriptionExpiresAt == "" {
		identity.SubscriptionExpiresAt = importedStringFromMaps(credentialMaps, "subscription_expires_at", "subscriptionExpiresAt", "plan_expires_at", "planExpiresAt")
	}
	if identity.PrivacyMode == "" {
		identity.PrivacyMode = importedStringFromMaps(credentialMaps, "privacy_mode", "privacyMode")
	}
	if idToken := importedStringFromMaps(credentialMaps, importedIDTokenKeys...); idToken != "" {
		mergeIdentityClaims(&identity, decodeJWTClaims(idToken))
	}
	if accessToken := importedStringFromMaps(credentialMaps, importedAccessTokenKeys...); accessToken != "" {
		mergeIdentityClaims(&identity, decodeJWTClaims(accessToken))
	}
	if account.AuthExpiresAt == "" {
		account.AuthExpiresAt = importedAuthExpiresAt(account.Credentials)
	}
	if identity.Email != "" || identity.Subject != "" || identity.Plan != "" || identity.OrganizationID != "" {
		if identity.Source == "" {
			identity.Source = fallbackSource
		}
		if identity.UpdatedAt == "" {
			identity.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		}
	}
	account.Identity = normalizeAccountIdentity(identity)
}

func decodeJWTClaims(token string) map[string]any {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 || parts[1] == "" {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil
		}
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return nil
	}
	return claims
}

func mergeIdentityClaims(identity *AccountIdentity, claims map[string]any) {
	if identity == nil || claims == nil {
		return
	}
	if identity.Email == "" {
		identity.Email = stringValue(claims, "email")
	}
	if identity.Subject == "" {
		identity.Subject = stringValue(claims, "sub", "user_id", "chatgpt_user_id")
	}
	if identity.Plan == "" {
		identity.Plan = stringValue(claims, "plan", "plan_type", "chatgpt_plan_type")
	}
	if identity.OrganizationID == "" {
		identity.OrganizationID = stringValue(claims, "organization_id", "org_id", "account_id", "chatgpt_account_id", "poid")
	}
	if authClaims := mapValue(claims, "https://api.openai.com/auth", "auth"); authClaims != nil {
		if identity.Plan == "" {
			identity.Plan = stringValue(authClaims, "chatgpt_plan_type", "plan_type", "plan")
		}
		if identity.OrganizationID == "" {
			identity.OrganizationID = stringValue(authClaims, "chatgpt_account_id", "organization_id", "poid")
		}
	}
}

func withActiveRequestCounts(accounts []UpstreamAccount) []UpstreamAccount {
	accountRun.Lock()
	defer accountRun.Unlock()
	quotaRun.RLock()
	defer quotaRun.RUnlock()
	result := make([]UpstreamAccount, len(accounts))
	for i, account := range accounts {
		result[i] = account
		result[i].ActiveRequests = accountRun.active[account.ID]
		if snapshot, ok := quotaRun.values[account.ID]; ok {
			result[i].Quota = snapshot
		}
	}
	return result
}

// ObserveUpstreamQuota records only recognised rate-limit response headers.
// It is intentionally passive: opening the account list never creates an
// extra upstream request. Persisting is throttled because many APIs return
// rate headers on every streaming response.
func ObserveUpstreamQuota(accountID, provider string, statusCode int, headers http.Header) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || headers == nil {
		return
	}
	snapshot := QuotaSnapshot{
		Source:     strings.TrimSpace(provider) + " 上游响应头",
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		StatusCode: statusCode,
	}
	if strings.TrimSpace(provider) == "" {
		snapshot.Source = "上游响应头"
	}
	snapshot.RequestsRemaining = firstHeader(headers,
		"X-RateLimit-Remaining-Requests", "Anthropic-Ratelimit-Requests-Remaining")
	snapshot.TokensRemaining = firstHeader(headers,
		"X-RateLimit-Remaining-Tokens", "Anthropic-Ratelimit-Tokens-Remaining")
	snapshot.RequestsReset = firstHeader(headers,
		"X-RateLimit-Reset-Requests", "Anthropic-Ratelimit-Requests-Reset")
	snapshot.TokensReset = firstHeader(headers,
		"X-RateLimit-Reset-Tokens", "Anthropic-Ratelimit-Tokens-Reset")
	snapshot.RetryAfter = firstHeader(headers, "Retry-After")
	snapshot.Available = snapshot.RequestsRemaining != "" || snapshot.TokensRemaining != "" ||
		snapshot.RequestsReset != "" || snapshot.TokensReset != "" || snapshot.RetryAfter != ""
	if !snapshot.Available {
		return
	}

	now := time.Now()
	quotaRun.Lock()
	quotaRun.values[accountID] = snapshot
	lastPersisted := quotaRun.lastPersisted[accountID]
	shouldPersist := lastPersisted.IsZero() || now.Sub(lastPersisted) >= time.Minute
	if shouldPersist {
		quotaRun.lastPersisted[accountID] = now
	}
	quotaRun.Unlock()
	if shouldPersist {
		_ = updateUpstreamAccount(accountID, func(account *UpstreamAccount) {
			account.Quota = snapshot
		})
	}
}

// SaveQuotaSnapshot stores a result from an explicit user-requested quota
// refresh. No proxy request path calls this helper automatically.
func SaveQuotaSnapshot(accountID string, snapshot QuotaSnapshot) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return fmt.Errorf("未找到该账户")
	}
	snapshot = normalizeQuotaSnapshot(snapshot)
	if snapshot.UpdatedAt == "" {
		snapshot.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	quotaRun.Lock()
	quotaRun.values[accountID] = snapshot
	quotaRun.lastPersisted[accountID] = time.Now()
	quotaRun.Unlock()
	return updateUpstreamAccount(accountID, func(account *UpstreamAccount) {
		account.Quota = snapshot
	})
}

func firstHeader(headers http.Header, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(headers.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func SaveUpstreamAccount(account UpstreamAccount) error {
	if isRawJSONAccountType(account.Type) {
		return fmt.Errorf("原始 JSON 凭据只能通过“导入账户 JSON”功能导入，不能作为 API Key 直接保存")
	}
	if isRefreshTokenAccountType(account.Type) {
		return fmt.Errorf("Refresh Token / Mobile RT 必须先兑换为 OAuth 访问令牌，不能作为 API Key 直接保存")
	}
	account = normalizeAccount(account)
	if account.EffectiveAPIKey() == "" {
		return fmt.Errorf("请填写 API Key、访问令牌或导入含凭据的 JSON")
	}
	accountsMu.Lock()
	defer accountsMu.Unlock()
	accounts, err := loadAccountsLocked()
	if err != nil {
		return err
	}
	for i, existing := range accounts {
		if existing.ID == account.ID {
			account.CreatedAt = existing.CreatedAt
			accounts[i] = account
			return saveAccountsLocked(accounts)
		}
	}
	accounts = append(accounts, account)
	return saveAccountsLocked(accounts)
}

func GetUpstreamAccount(id string) (UpstreamAccount, error) {
	accountsMu.RLock()
	defer accountsMu.RUnlock()
	accounts, err := loadAccountsLocked()
	if err != nil {
		return UpstreamAccount{}, err
	}
	for _, account := range accounts {
		if account.ID == strings.TrimSpace(id) {
			return account, nil
		}
	}
	return UpstreamAccount{}, fmt.Errorf("未找到该账户")
}

func DeleteUpstreamAccount(id string) error {
	id = strings.TrimSpace(id)
	accountsMu.Lock()
	defer accountsMu.Unlock()
	accounts, err := loadAccountsLocked()
	if err != nil {
		return err
	}
	result := accounts[:0]
	found := false
	for _, account := range accounts {
		if account.ID == id {
			found = true
			continue
		}
		result = append(result, account)
	}
	if !found {
		return fmt.Errorf("未找到该账户")
	}
	if err := saveAccountsLocked(result); err != nil {
		return err
	}
	accountRun.Lock()
	delete(accountRun.active, id)
	accountRun.Unlock()
	return nil
}

func SetUpstreamAccountEnabled(id string, enabled bool) error {
	return updateUpstreamAccount(id, func(account *UpstreamAccount) {
		account.Enabled = enabled
		if enabled {
			account.LastError = ""
			account.FailureCount = 0
			account.CooldownUntil = ""
		}
	})
}

func updateUpstreamAccount(id string, update func(*UpstreamAccount)) error {
	id = strings.TrimSpace(id)
	accountsMu.Lock()
	defer accountsMu.Unlock()
	accounts, err := loadAccountsLocked()
	if err != nil {
		return err
	}
	for i := range accounts {
		if accounts[i].ID != id {
			continue
		}
		update(&accounts[i])
		accounts[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return saveAccountsLocked(accounts)
	}
	return fmt.Errorf("未找到该账户")
}

func saveAccountsLocked(accounts []UpstreamAccount) error {
	if accountsFile == "" {
		return fmt.Errorf("账户存储尚未初始化")
	}
	data, err := json.MarshalIndent(accountsStore{Accounts: accounts}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(accountsFile), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(accountsFile), ".upstream-accounts-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceStorageFile(tempPath, accountsFile); err != nil {
		return err
	}
	return os.Chmod(accountsFile, 0o600)
}

// AcquireAccountForModel reserves the best usable bound account. Linked
// accounts are sorted by priority, current concurrency and least-recent use;
// accounts in a temporary cooldown are skipped. A model without bindings
// deliberately retains legacy per-model credentials for backward compatibility.
func AcquireAccountForModel(model CustomModel, excluded map[string]struct{}) (CustomModel, *AccountLease, error) {
	ids := normalizedAccountIDs(model.AccountIDs)
	if len(ids) == 0 {
		return model, nil, nil
	}
	// Refresh near-expiry OAuth credentials before evaluating the pool. Failure
	// is recorded on that account and the remaining bound accounts stay eligible.
	for _, accountID := range ids {
		if _, skipped := excluded[accountID]; skipped {
			continue
		}
		_ = EnsureAccountAccessToken(context.Background(), accountID)
	}
	accountsMu.RLock()
	accounts, err := loadAccountsLocked()
	accountsMu.RUnlock()
	if err != nil {
		return model, nil, err
	}
	now := time.Now()
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	type candidate struct {
		account UpstreamAccount
		active  int
	}
	var candidates []candidate
	accountRun.Lock()
	for _, account := range accounts {
		if _, ok := allowed[account.ID]; !ok {
			continue
		}
		if _, skip := excluded[account.ID]; skip || !accountUsable(account, now) {
			continue
		}
		active := accountRun.active[account.ID]
		if active >= account.MaxConcurrency {
			continue
		}
		candidates = append(candidates, candidate{account: account, active: active})
	}
	if len(candidates) > 0 {
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].account.Priority != candidates[j].account.Priority {
				return candidates[i].account.Priority < candidates[j].account.Priority
			}
			if candidates[i].active != candidates[j].active {
				return candidates[i].active < candidates[j].active
			}
			return candidates[i].account.LastUsedAt < candidates[j].account.LastUsedAt
		})
		accountRun.active[candidates[0].account.ID]++
	}
	accountRun.Unlock()
	if len(candidates) == 0 {
		return model, nil, fmt.Errorf("绑定账户当前均不可调度：请检查额度、冷却时间或并发限制")
	}
	selected := candidates[0].account
	_ = updateUpstreamAccount(selected.ID, func(account *UpstreamAccount) {
		account.LastUsedAt = now.UTC().Format(time.RFC3339)
	})
	return selected.ToModel(model), &AccountLease{ID: selected.ID}, nil
}

func normalizedAccountIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func accountUsable(account UpstreamAccount, now time.Time) bool {
	if !account.Enabled || account.EffectiveAPIKey() == "" {
		return false
	}
	if until, ok := parseAccountTime(account.ExpiresAt); ok && !until.After(now) {
		return false
	}
	if expiresAt, ok := parseAccountTime(account.AuthExpiresAt); ok && !expiresAt.After(now) {
		return false
	}
	if until, ok := parseAccountTime(account.CooldownUntil); ok && until.After(now) {
		return false
	}
	return true
}

func parseAccountTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	return parsed, err == nil
}

// Finish releases an in-flight reservation and records health. statusCode is
// 0 for a network interruption; a 2xx status is a successful request.
func (lease *AccountLease) Finish(statusCode int, retryAfter, failure string) {
	if lease == nil || lease.ID == "" {
		return
	}
	lease.once.Do(func() {
		accountRun.Lock()
		if accountRun.active[lease.ID] <= 1 {
			delete(accountRun.active, lease.ID)
		} else {
			accountRun.active[lease.ID]--
		}
		accountRun.Unlock()
		_ = updateUpstreamAccount(lease.ID, func(account *UpstreamAccount) {
			now := time.Now().UTC()
			if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices && strings.TrimSpace(failure) == "" {
				account.LastSuccessAt = now.Format(time.RFC3339)
				account.LastError = ""
				account.FailureCount = 0
				account.CooldownUntil = ""
				return
			}
			account.FailureCount++
			account.LastError = shortAccountError(failure, statusCode)
			account.CooldownUntil = now.Add(accountCooldown(statusCode, retryAfter, account.FailureCount)).Format(time.RFC3339)
		})
	})
}

func shortAccountError(failure string, statusCode int) string {
	failure = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(failure, "\r", " "), "\n", " "))
	if failure == "" {
		if statusCode > 0 {
			failure = fmt.Sprintf("上游返回 HTTP %d", statusCode)
		} else {
			failure = "上游连接中断"
		}
	}
	if len([]rune(failure)) > 240 {
		failure = string([]rune(failure)[:240]) + "…"
	}
	return failure
}

func accountCooldown(statusCode int, retryAfter string, failures int) time.Duration {
	if retryAfter = strings.TrimSpace(retryAfter); retryAfter != "" {
		if seconds, err := strconv.ParseFloat(retryAfter, 64); err == nil && seconds >= 0 {
			return boundedAccountCooldown(time.Duration(seconds * float64(time.Second)))
		}
		if at, err := http.ParseTime(retryAfter); err == nil {
			return boundedAccountCooldown(time.Until(at))
		}
	}
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return 10 * time.Minute
	case http.StatusTooManyRequests:
		return 90 * time.Second
	case 0, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		delay := 5 * time.Second * time.Duration(1<<minInt(failures-1, 5))
		return boundedAccountCooldown(delay)
	default:
		return 20 * time.Second
	}
}

func boundedAccountCooldown(value time.Duration) time.Duration {
	if value < 3*time.Second {
		return 3 * time.Second
	}
	if value > 30*time.Minute {
		return 30 * time.Minute
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ImportUpstreamAccounts accepts a single JSON object, an array, XIASS's
// {accounts:[...]} export, or a credentials object. It intentionally does not
// log raw JSON because it may contain refresh tokens or API secrets.
func ImportUpstreamAccounts(raw string) AccountImportResult {
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return AccountImportResult{Message: "账户 JSON 无法解析"}
	}
	objects := accountImportObjects(decoded)
	if len(objects) == 0 {
		return AccountImportResult{Message: "JSON 中没有找到可导入的账户"}
	}
	added := 0
	for _, object := range objects {
		account, ok := accountFromImportObject(object)
		if !ok || account.EffectiveAPIKey() == "" {
			continue
		}
		if err := SaveUpstreamAccount(account); err != nil {
			return AccountImportResult{Message: err.Error(), Added: added}
		}
		added++
	}
	if added == 0 {
		return AccountImportResult{Message: "未发现 API Key、访问令牌或 OAuth 凭据", Added: 0}
	}
	return AccountImportResult{OK: true, Added: added, Message: fmt.Sprintf("已导入 %d 个账户", added)}
}

func accountImportObjects(value any) []map[string]any {
	switch current := value.(type) {
	case []any:
		result := make([]map[string]any, 0, len(current))
		for _, item := range current {
			result = append(result, accountImportObjects(item)...)
		}
		return result
	case map[string]any:
		for _, key := range []string{"accounts", "data", "items"} {
			if children, ok := current[key]; ok {
				if result := accountImportObjects(children); len(result) > 0 {
					return result
				}
			}
		}
		return []map[string]any{current}
	default:
		return nil
	}
}

func accountFromImportObject(value map[string]any) (UpstreamAccount, bool) {
	credentials := credentialsFromImportObject(value)
	credentialMaps := importedCredentialMaps(value)
	identity := importedAccountIdentity(value)
	provider := importedStringFromMaps(credentialMaps, "provider", "platform", "vendor")
	apiURL := importedStringFromMaps(credentialMaps, "apiUrl", "api_url", "baseUrl", "base_url", "endpoint", "url")
	name := stringValue(value, "name", "displayName", "label", "email")
	if name == "" {
		name = identity.Email
	}
	account := UpstreamAccount{
		Name:            name,
		Notes:           stringValue(value, "notes", "remark", "description"),
		Provider:        provider,
		Type:            stringValue(value, "type", "authType", "auth_type"),
		APIURL:          apiURL,
		EndpointMode:    stringValue(value, "endpointMode", "endpoint_mode", "urlMode", "url_mode"),
		APIStyle:        stringValue(value, "apiStyle", "api_style", "mode"),
		MessagePathMode: stringValue(value, "messagePathMode", "message_path_mode"),
		AuthMode:        stringValue(value, "authMode", "auth_mode"),
		AuthHeader:      stringValue(value, "authHeader", "auth_header"),
		OAuth:           importedOAuthConfiguration(value),
		APIKey:          importedStringFromMaps([]map[string]any{value}, importedAPIKeyKeys...),
		Credentials:     credentials,
		Identity:        identity,
		AuthExpiresAt:   importedAuthExpiresAt(value),
		Enabled:         true,
		Priority:        intValue(value, "priority"),
		MaxConcurrency:  intValue(value, "maxConcurrency", "max_concurrency", "concurrency"),
		ExpiresAt:       importedAccountExpiresAt(value),
	}
	if headers := mapValue(value, "headers", "extraHeaders", "extra_headers"); headers != nil {
		account.Headers = stringMap(headers)
	}
	if importedOAuthCredentialsPresent(credentials) && (account.Type == "" || isRawJSONAccountType(account.Type) || isRefreshTokenAccountType(account.Type)) {
		// A JSON export is a transport format, not an auth scheme. Once we have
		// extracted OAuth credentials it must become an OAuth account so expiry
		// handling and automatic refresh are enabled after import.
		account.Type = "oauth"
	} else if isRawJSONAccountType(account.Type) {
		// A JSON envelope that contains a normal API key is still a valid import.
		// Preserve the distinction at the direct-save boundary, but normalize it
		// here because the importer has already extracted its credential safely.
		account.Type = "api_key"
	} else if account.Type == "" {
		account.Type = "api_key"
	}
	account = normalizeAccount(account)
	return account, account.EffectiveAPIKey() != ""
}

func mapValue(value map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if child, ok := value[key].(map[string]any); ok {
			return cloneAnyMap(child)
		}
	}
	return nil
}

func stringValue(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if candidate, ok := value[key].(string); ok && strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

func intValue(value map[string]any, keys ...string) int {
	for _, key := range keys {
		switch candidate := value[key].(type) {
		case float64:
			return int(candidate)
		case int:
			return candidate
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(candidate)); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func stringMap(value map[string]any) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		switch current := item.(type) {
		case string:
			result[key] = current
		case float64, bool:
			result[key] = fmt.Sprint(current)
		}
	}
	return result
}
