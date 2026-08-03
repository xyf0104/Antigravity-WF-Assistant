package storage

import (
	"crypto/rand"
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
	APIKey          string            `json:"apiKey,omitempty"`
	Credentials     map[string]any    `json:"credentials,omitempty"`
	Enabled         bool              `json:"enabled"`
	Priority        int               `json:"priority"`
	MaxConcurrency  int               `json:"maxConcurrency"`
	ExpiresAt       string            `json:"expiresAt,omitempty"`
	LastUsedAt      string            `json:"lastUsedAt,omitempty"`
	LastSuccessAt   string            `json:"lastSuccessAt,omitempty"`
	LastError       string            `json:"lastError,omitempty"`
	FailureCount    int               `json:"failureCount,omitempty"`
	CooldownUntil   string            `json:"cooldownUntil,omitempty"`
	CreatedAt       string            `json:"createdAt,omitempty"`
	UpdatedAt       string            `json:"updatedAt,omitempty"`
	ActiveRequests  int               `json:"activeRequests,omitempty"`
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
		account.Credentials = cloneAnyMap(account.Credentials)
	}
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
	case "setup_token", "setup-token":
		return "setup_token"
	case "custom_header", "custom-header":
		return "custom_header"
	case "json", "json_import", "import":
		return "json"
	default:
		return "api_key"
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
	for _, key := range []string{"api_key", "apiKey", "access_token", "accessToken", "token", "setup_token", "setupToken", "secret"} {
		if value, ok := account.Credentials[key]; ok {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
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
	if account.MaxConcurrency <= 0 {
		account.MaxConcurrency = 2
	}
	if account.MaxConcurrency > 32 {
		account.MaxConcurrency = 32
	}
	return account
}

func withActiveRequestCounts(accounts []UpstreamAccount) []UpstreamAccount {
	accountRun.Lock()
	defer accountRun.Unlock()
	result := make([]UpstreamAccount, len(accounts))
	for i, account := range accounts {
		result[i] = account
		result[i].ActiveRequests = accountRun.active[account.ID]
	}
	return result
}

func SaveUpstreamAccount(account UpstreamAccount) error {
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
	credentials := mapValue(value, "credentials", "credential", "auth", "tokens")
	if credentials == nil {
		credentials = cloneAnyMap(value)
	}
	provider := stringValue(value, "provider", "platform", "vendor")
	if provider == "" {
		provider = stringValue(credentials, "provider", "platform")
	}
	apiURL := stringValue(value, "apiUrl", "api_url", "baseUrl", "base_url", "endpoint", "url")
	if apiURL == "" {
		apiURL = stringValue(credentials, "apiUrl", "api_url", "baseUrl", "base_url", "endpoint", "url")
	}
	account := UpstreamAccount{
		Name:            stringValue(value, "name", "displayName", "label", "email"),
		Notes:           stringValue(value, "notes", "remark", "description"),
		Provider:        provider,
		Type:            stringValue(value, "type", "authType", "auth_type"),
		APIURL:          apiURL,
		EndpointMode:    stringValue(value, "endpointMode", "endpoint_mode", "urlMode", "url_mode"),
		APIStyle:        stringValue(value, "apiStyle", "api_style", "mode"),
		MessagePathMode: stringValue(value, "messagePathMode", "message_path_mode"),
		AuthMode:        stringValue(value, "authMode", "auth_mode"),
		AuthHeader:      stringValue(value, "authHeader", "auth_header"),
		APIKey:          stringValue(value, "apiKey", "api_key", "accessToken", "access_token", "token", "setupToken", "setup_token"),
		Credentials:     credentials,
		Enabled:         true,
		Priority:        intValue(value, "priority"),
		MaxConcurrency:  intValue(value, "maxConcurrency", "max_concurrency", "concurrency"),
		ExpiresAt:       stringValue(value, "expiresAt", "expires_at"),
	}
	if headers := mapValue(value, "headers", "extraHeaders", "extra_headers"); headers != nil {
		account.Headers = stringMap(headers)
	}
	if account.Type == "" {
		if _, exists := credentials["access_token"]; exists {
			account.Type = "oauth"
		} else {
			account.Type = "api_key"
		}
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
