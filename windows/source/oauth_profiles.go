package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"antigravity-byok/internal/oauthflow"
	"antigravity-byok/internal/storage"
	"antigravity-byok/internal/upstream"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	oauthProfileReady             = "ready"
	oauthProfileManual            = "manual"
	oauthProfileNeedsClientID     = "requires_client_id"
	oauthProfileCustomOnly        = "custom_only"
	oauthResultRetention          = 10 * time.Minute
	defaultCustomOAuthRedirectURI = "http://localhost:1455/auth/callback"
)

// errOAuthLoopbackPortUnavailable is deliberately narrower than invalid
// redirect configuration. A port collision is recoverable by manual callback
// completion; malformed/non-loopback configuration remains a real error.
var errOAuthLoopbackPortUnavailable = errors.New("OAuth loopback port unavailable")

// OAuthProviderProfile is a non-secret preset for an Authorization Code +
// PKCE flow. It deliberately has no client-secret, access token, refresh
// token, private host, or provider-private endpoint field.
type OAuthProviderProfile struct {
	ID                      string                     `json:"id"`
	Name                    string                     `json:"name"`
	Description             string                     `json:"description"`
	Provider                string                     `json:"provider"`
	APIURL                  string                     `json:"apiUrl"`
	APIStyle                string                     `json:"apiStyle"`
	AuthMode                string                     `json:"authMode"`
	OAuth                   storage.OAuthConfiguration `json:"oauth"`
	AuthorizationParameters map[string]string          `json:"authorizationParameters,omitempty"`
	RefreshScopes           string                     `json:"refreshScopes,omitempty"`
	NonceParameter          string                     `json:"nonceParameter,omitempty"`
	// PKCEVerifierFormat is provider interoperability metadata consumed only by
	// native OAuth code. It is intentionally not exported to the renderer: the
	// renderer never receives a verifier and cannot choose one.
	PKCEVerifierFormat oauthflow.PKCEVerifierFormat `json:"-"`
	AutoLoopback       bool                         `json:"autoLoopback"`
	Available          string                       `json:"available"`
	RequiresClientID   bool                         `json:"requiresClientId"`
	Message            string                       `json:"message,omitempty"`
}

// OAuthProfileApplyResult returns a redacted account draft after applying a
// preset. Existing account credentials are intentionally never echoed back.
type OAuthProfileApplyResult struct {
	OK      bool                    `json:"ok"`
	Message string                  `json:"message"`
	Profile OAuthProviderProfile    `json:"profile,omitempty"`
	Account storage.UpstreamAccount `json:"account"`
}

// OAuthLoginProfile is the deliberately minimal profile projection delivered
// to the renderer. It avoids duplicating client configuration in JavaScript,
// and can never expose a credential even if a future provider profile gains
// additional server-side metadata.
type OAuthLoginProfile struct {
	ID                       string `json:"id"`
	Label                    string `json:"label"`
	Description              string `json:"description,omitempty"`
	Provider                 string `json:"provider,omitempty"`
	AutomaticCallback        bool   `json:"automaticCallback"`
	ManualCompletionRequired bool   `json:"manualCompletionRequired"`
	RequiresClientID         bool   `json:"requiresClientId"`
	Available                string `json:"available"`
	Message                  string `json:"message,omitempty"`
}

// OAuthAuthorizationStatus is safe to poll or emit to the renderer. Result
// contains only persistence/identity metadata, never OAuth credentials.
type OAuthAuthorizationStatus struct {
	State     string                `json:"state"`
	Message   string                `json:"message"`
	SessionID string                `json:"sessionId,omitempty"`
	ExpiresAt string                `json:"expiresAt,omitempty"`
	Result    OAuthCompletionResult `json:"result,omitempty"`
}

type oauthAuthorizationRecord struct {
	result  OAuthCompletionResult
	expires time.Time
}

type oauthLoopbackListener struct {
	listener     net.Listener
	server       *http.Server
	path         string
	once         sync.Once
	completionMu sync.Mutex
}

func (listener *oauthLoopbackListener) start(handler http.Handler) {
	if listener == nil || listener.listener == nil {
		return
	}
	listener.server = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       10 * time.Second,
	}
	go func() {
		_ = listener.server.Serve(listener.listener)
	}()
}

func (listener *oauthLoopbackListener) Close() {
	if listener == nil {
		return
	}
	listener.once.Do(func() {
		if listener.server != nil {
			_ = listener.server.Close()
			return
		}
		if listener.listener != nil {
			_ = listener.listener.Close()
		}
	})
}

// GetOAuthProviderProfiles exposes only public OAuth metadata. Profiles that
// need a client secret or an unverified/private callback are explicitly
// labelled instead of pretending they are one-click desktop flows.
func (a *App) GetOAuthProviderProfiles() []OAuthProviderProfile {
	profiles := builtInOAuthProviderProfiles()
	result := make([]OAuthProviderProfile, len(profiles))
	for index, profile := range profiles {
		result[index] = cloneOAuthProviderProfile(profile)
	}
	return result
}

// GetOAuthLoginProfiles is the renderer-compatible, redacted list of OAuth
// login choices. Custom OAuth is intentionally omitted because it is already
// represented by the editable advanced form.
func (a *App) GetOAuthLoginProfiles() []OAuthLoginProfile {
	profiles := builtInOAuthProviderProfiles()
	result := make([]OAuthLoginProfile, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Available == oauthProfileCustomOnly {
			continue
		}
		result = append(result, OAuthLoginProfile{
			ID:                       profile.ID,
			Label:                    profile.Name,
			Description:              profile.Description,
			Provider:                 profile.Provider,
			AutomaticCallback:        profile.AutoLoopback,
			ManualCompletionRequired: !profile.AutoLoopback,
			RequiresClientID:         profile.RequiresClientID,
			Available:                profile.Available,
			Message:                  profile.Message,
		})
	}
	return result
}

// ApplyOAuthProviderProfile fills an account draft with a profile while
// retaining a user-supplied OAuth client ID, endpoint URLs, redirect URI, and
// scopes. The caller can therefore use a preset as a starting point and still
// modify every public OAuth setting.
func (a *App) ApplyOAuthProviderProfile(profileID string, draft storage.UpstreamAccount) OAuthProfileApplyResult {
	profile, found := oauthProviderProfile(profileID)
	if !found {
		return OAuthProfileApplyResult{Message: "未找到 OAuth 预设"}
	}
	if profile.Available == oauthProfileCustomOnly {
		return OAuthProfileApplyResult{Message: profile.Message, Profile: cloneOAuthProviderProfile(profile)}
	}
	applied := applyOAuthProviderProfile(draft, profile)
	return OAuthProfileApplyResult{
		OK:      true,
		Message: "已应用 OAuth 预设；仍可修改公开客户端 ID、授权地址、令牌地址、回调地址和范围。",
		Profile: cloneOAuthProviderProfile(profile),
		Account: redactOAuthDraft(applied),
	}
}

// BeginOAuthProviderAuthorization applies a profile and starts its PKCE flow.
// Profiles with a loopback redirect receive an automatic local callback;
// manual profiles retain the existing copy-and-paste fallback.
func (a *App) BeginOAuthProviderAuthorization(profileID string, draft storage.UpstreamAccount) OAuthAuthorizationResult {
	profile, found := oauthProviderProfile(profileID)
	if !found {
		return OAuthAuthorizationResult{Message: "未找到 OAuth 预设"}
	}
	if profile.Available == oauthProfileCustomOnly {
		return OAuthAuthorizationResult{Message: profile.Message}
	}
	draft = applyOAuthProviderProfile(draft, profile)
	if profile.RequiresClientID && strings.TrimSpace(draft.OAuth.ClientID) == "" {
		return OAuthAuthorizationResult{Message: "该预设需要你自己的公开 OAuth Client ID；不会要求或保存 Client Secret。"}
	}
	return a.beginProfileOAuthAuthorization(profile, draft)
}

// StartOAuthProviderAuthorization retains the frontend API name while the
// more explicit BeginOAuthProviderAuthorization remains available to native
// integrations.
func (a *App) StartOAuthProviderAuthorization(profileID string, draft storage.UpstreamAccount) OAuthAuthorizationResult {
	return a.BeginOAuthProviderAuthorization(profileID, draft)
}

// GetOAuthAuthorizationStatus lets the renderer poll a loopback flow without
// ever receiving the exchanged access or refresh token.
func (a *App) GetOAuthAuthorizationStatus(sessionID string) OAuthAuthorizationStatus {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return OAuthAuthorizationStatus{State: "unknown", Message: "缺少 OAuth 会话标识"}
	}
	now := time.Now()
	a.oauthMu.Lock()
	a.ensureOAuthMapsLocked()
	a.discardExpiredOAuthSessionsLocked(now)
	// A completed result wins over every other state and is safe to return to a
	// duplicate browser redirect or a manual fallback submission. A failed
	// transient callback must not hide the still-live PKCE session, though: the
	// user can paste the callback/code again after a network hiccup.
	if record, found := a.oauthResults[sessionID]; found && record.result.OK {
		a.oauthMu.Unlock()
		return OAuthAuthorizationStatus{
			State: "completed", Message: record.result.Message, SessionID: sessionID,
			ExpiresAt: record.expires.UTC().Format(time.RFC3339), Result: record.result,
		}
	}
	if session, found := a.oauthSessions[sessionID]; found && session != nil {
		a.oauthMu.Unlock()
		return OAuthAuthorizationStatus{
			State: "pending", Message: "正在等待浏览器完成 OAuth 授权。", SessionID: sessionID,
			ExpiresAt: session.expires.UTC().Format(time.RFC3339),
		}
	}
	if record, found := a.oauthResults[sessionID]; found {
		a.oauthMu.Unlock()
		return OAuthAuthorizationStatus{
			State: "failed", Message: record.result.Message, SessionID: sessionID,
			ExpiresAt: record.expires.UTC().Format(time.RFC3339), Result: record.result,
		}
	}
	a.oauthMu.Unlock()
	return OAuthAuthorizationStatus{State: "expired", Message: "OAuth 会话不存在或已过期，请重新开始授权。", SessionID: sessionID}
}

func (a *App) beginProfileOAuthAuthorization(profile OAuthProviderProfile, draft storage.UpstreamAccount) OAuthAuthorizationResult {
	if strings.TrimSpace(draft.ID) != "" {
		existing, err := storage.GetUpstreamAccount(draft.ID)
		if err != nil {
			return OAuthAuthorizationResult{Message: err.Error()}
		}
		if strings.TrimSpace(draft.APIKey) == "" {
			draft.APIKey = existing.APIKey
		}
		if draft.Credentials == nil {
			draft.Credentials = existing.Credentials
		}
	}
	if strings.TrimSpace(draft.ID) == "" {
		accountID, err := newOAuthImportedAccountID()
		if err != nil {
			return OAuthAuthorizationResult{Message: "无法创建本地账户标识：" + err.Error()}
		}
		draft.ID = accountID
	}

	parameters := cloneOAuthParameters(profile.AuthorizationParameters)
	if parameterName := strings.TrimSpace(profile.NonceParameter); parameterName != "" {
		nonce, err := newOAuthNonce()
		if err != nil {
			return OAuthAuthorizationResult{Message: "无法创建 OAuth nonce：" + err.Error()}
		}
		if parameters == nil {
			parameters = make(map[string]string)
		}
		parameters[parameterName] = nonce
	}

	automaticCallback := profile.AutoLoopback && isLoopbackOAuthRedirectURI(draft.OAuth.RedirectURI)
	loopbackFallback := false
	var callback *oauthLoopbackListener
	if automaticCallback {
		originalRedirectURI := draft.OAuth.RedirectURI
		resolvedRedirectURI := ""
		var err error
		callback, resolvedRedirectURI, err = newOAuthLoopbackListener(originalRedirectURI)
		if err != nil {
			if errors.Is(err, errOAuthLoopbackPortUnavailable) {
				// The redirect URI remains registered and usable in the browser;
				// keep the one-time PKCE session and let the user paste the full
				// callback URL or raw code instead of failing the entire login.
				automaticCallback = false
				loopbackFallback = true
				draft.OAuth.RedirectURI = originalRedirectURI
			} else {
				return OAuthAuthorizationResult{Message: "无法启动 OAuth 本地回调：" + err.Error()}
			}
		} else {
			draft.OAuth.RedirectURI = resolvedRedirectURI
		}
	}
	closeCallback := func() {
		if callback != nil {
			callback.Close()
		}
	}

	flow, err := oauthflow.New(oauthflow.Config{
		AuthorizationURL:        draft.OAuth.AuthorizationURL,
		TokenURL:                draft.OAuth.TokenURL,
		PublicClientID:          draft.OAuth.ClientID,
		RedirectURI:             draft.OAuth.RedirectURI,
		Scopes:                  strings.Fields(draft.OAuth.Scopes),
		RefreshScopes:           strings.Fields(draft.OAuth.RefreshScopes),
		PKCEVerifierFormat:      profile.PKCEVerifierFormat,
		AuthorizationParameters: parameters,
	})
	if err != nil {
		closeCallback()
		return OAuthAuthorizationResult{Message: oauthErrorMessage(err)}
	}
	config := upstream.ConfigFromAccount(draft)
	config.APIKey = "oauth-pending"
	if err := upstream.ValidateConfig(config); err != nil {
		closeCallback()
		return OAuthAuthorizationResult{Message: err.Error()}
	}
	authorization, err := flow.Begin()
	if err != nil {
		closeCallback()
		return OAuthAuthorizationResult{Message: oauthErrorMessage(err)}
	}

	a.oauthMu.Lock()
	a.ensureOAuthMapsLocked()
	a.discardExpiredOAuthSessionsLocked(time.Now())
	a.oauthSessions[authorization.SessionID] = &pendingOAuthSession{
		flow: flow, account: draft, state: authorization.State, expires: authorization.ExpiresAt,
	}
	if callback != nil {
		a.oauthLoopbacks[authorization.SessionID] = callback
	}
	a.oauthMu.Unlock()

	if callback != nil {
		callback.start(a.loopbackOAuthCallbackHandler(authorization.SessionID, callback))
	}
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, authorization.URL)
	}
	message := "已在浏览器打开授权页；授权后请粘贴完整回调 URL 或授权码。"
	if automaticCallback {
		message = "已在浏览器打开授权页；授权完成后将自动返回助手。若浏览器没有自动回到助手，可粘贴完整回调 URL 或授权码完成。"
	} else if loopbackFallback {
		message = "本机 OAuth 回调端口被占用；已继续打开授权页。授权后请从浏览器地址栏复制完整回调 URL 或授权码并粘贴完成。"
	}
	return OAuthAuthorizationResult{
		OK:                       true,
		Message:                  message,
		SessionID:                authorization.SessionID,
		AuthorizationURL:         authorization.URL,
		RedirectURI:              draft.OAuth.RedirectURI,
		ProfileID:                profile.ID,
		AutomaticCallback:        automaticCallback,
		ManualCompletionRequired: !automaticCallback,
		ExpiresAt:                authorization.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func (a *App) loopbackOAuthCallbackHandler(sessionID string, callback *oauthLoopbackListener) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !isLoopbackRequest(request) {
			http.Error(writer, "loopback callback only", http.StatusForbidden)
			return
		}
		if request.URL.Path != callback.path {
			http.NotFound(writer, request)
			return
		}
		callbackQuery := request.URL.Query().Encode()
		parsed, err := oauthflow.ExtractCallback(callbackQuery)
		if err != nil {
			http.Error(writer, "authorization was not completed; return to Antigravity WF助手", http.StatusBadRequest)
			return
		}
		expectedState := a.oauthSessionState(sessionID)
		if expectedState == "" {
			if result, found := a.completedOAuthResult(sessionID); found && result.OK {
				writeOAuthCompletionPage(writer, true)
				return
			}
			http.Error(writer, "OAuth session is unavailable", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(parsed.State) == "" || !oauthStatesMatch(expectedState, parsed.State) {
			// Do not consume the session or call a token endpoint for a malformed
			// loopback request. The legitimate browser callback can still arrive.
			http.Error(writer, "invalid OAuth callback state", http.StatusBadRequest)
			return
		}

		// A listener can receive duplicate browser redirects. Serialising valid
		// exchanges keeps a losing duplicate from overwriting the successful
		// completion status with "exchange already in progress".
		callback.completionMu.Lock()
		result := a.CompleteOAuthAuthorization(sessionID, callbackQuery)
		callback.completionMu.Unlock()
		if !result.OK {
			http.Error(writer, "authorization could not be completed; return to Antigravity WF助手", http.StatusBadGateway)
			return
		}
		writeOAuthCompletionPage(writer, false)
		go a.closeOAuthLoopback(sessionID)
	})
}

func (a *App) oauthSessionState(sessionID string) string {
	a.oauthMu.Lock()
	defer a.oauthMu.Unlock()
	if session := a.oauthSessions[sessionID]; session != nil {
		return session.state
	}
	return ""
}

func (a *App) completedOAuthResult(sessionID string) (OAuthCompletionResult, bool) {
	a.oauthMu.Lock()
	defer a.oauthMu.Unlock()
	a.ensureOAuthMapsLocked()
	record, found := a.oauthResults[strings.TrimSpace(sessionID)]
	if !found || !record.result.OK {
		return OAuthCompletionResult{}, false
	}
	return record.result, true
}

func writeOAuthCompletionPage(writer http.ResponseWriter, duplicate bool) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	message := "授权完成，可以返回 Antigravity WF助手。"
	if duplicate {
		message = "该授权已完成，可以返回 Antigravity WF助手。"
	}
	_, _ = writer.Write([]byte("<!doctype html><title>Antigravity WF助手</title><p>" + message + "</p>"))
}

func (a *App) recordOAuthAuthorizationResult(sessionID string, result OAuthCompletionResult) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	expires := time.Now().Add(oauthResultRetention)
	a.oauthMu.Lock()
	a.ensureOAuthMapsLocked()
	if existing, found := a.oauthResults[sessionID]; found && existing.result.OK && !result.OK {
		// A late duplicate must never replace a successful completion with an
		// error status in the polling/event contract.
		a.oauthMu.Unlock()
		return
	}
	a.oauthResults[sessionID] = oauthAuthorizationRecord{result: result, expires: expires}
	a.oauthMu.Unlock()
	status := OAuthAuthorizationStatus{
		State: "failed", Message: result.Message, SessionID: sessionID,
		ExpiresAt: expires.UTC().Format(time.RFC3339), Result: result,
	}
	if result.OK {
		status.State = "completed"
	}
	if a.ctx != nil {
		// Keep the status event for polling-capable native integrations, while
		// the flat completed event is the stable renderer contract.
		runtime.EventsEmit(a.ctx, "wf:oauth-authorization", status)
		runtime.EventsEmit(a.ctx, "wf:oauth-completed", redactOAuthCompletionEvent(sessionID, result))
	}
}

func (a *App) closeOAuthLoopback(sessionID string) {
	a.oauthMu.Lock()
	callback := a.oauthLoopbacks[sessionID]
	delete(a.oauthLoopbacks, sessionID)
	a.oauthMu.Unlock()
	if callback != nil {
		callback.Close()
	}
}

func (a *App) stopOAuthLoopbacks() {
	a.oauthMu.Lock()
	callbacks := make([]*oauthLoopbackListener, 0, len(a.oauthLoopbacks))
	for sessionID, callback := range a.oauthLoopbacks {
		delete(a.oauthLoopbacks, sessionID)
		callbacks = append(callbacks, callback)
	}
	a.oauthMu.Unlock()
	for _, callback := range callbacks {
		callback.Close()
	}
}

func (a *App) ensureOAuthMapsLocked() {
	if a.oauthSessions == nil {
		a.oauthSessions = make(map[string]*pendingOAuthSession)
	}
	if a.oauthResults == nil {
		a.oauthResults = make(map[string]oauthAuthorizationRecord)
	}
	if a.oauthLoopbacks == nil {
		a.oauthLoopbacks = make(map[string]*oauthLoopbackListener)
	}
}

func newOAuthLoopbackListener(rawRedirectURI string) (*oauthLoopbackListener, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawRedirectURI))
	if err != nil || parsed == nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, "", fmt.Errorf("回调地址无效")
	}
	host := strings.ToLower(strings.Trim(parsed.Hostname(), "[]"))
	if parsed.Scheme != "http" || (host != "127.0.0.1" && host != "::1" && host != "localhost") {
		return nil, "", fmt.Errorf("自动回调仅支持 http://127.0.0.1、http://[::1] 或 http://localhost")
	}
	portText := parsed.Port()
	if portText == "" {
		return nil, "", fmt.Errorf("回调地址需要端口")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		return nil, "", fmt.Errorf("回调端口无效")
	}
	callbackPath := parsed.Path
	if callbackPath == "" {
		callbackPath = "/"
		parsed.Path = callbackPath
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, "", fmt.Errorf("%w", errOAuthLoopbackPortUnavailable)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	parsed.Host = net.JoinHostPort(host, strconv.Itoa(actualPort))
	return &oauthLoopbackListener{listener: listener, path: callbackPath}, parsed.String(), nil
}

// isLoopbackOAuthRedirectURI intentionally only decides whether a profile may
// use automatic callback handling. Full validation, including port and path
// checks, remains in newOAuthLoopbackListener so malformed loopback settings
// receive a useful error instead of silently falling back to a manual flow.
// A user who changes an auto-profile to a registered external HTTPS callback
// can therefore still complete OAuth by pasting the returned callback URL.
func isLoopbackOAuthRedirectURI(rawRedirectURI string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawRedirectURI))
	if err != nil || parsed == nil || !strings.EqualFold(parsed.Scheme, "http") {
		return false
	}
	host := strings.ToLower(strings.Trim(parsed.Hostname(), "[]"))
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func isLoopbackRequest(request *http.Request) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func oauthStatesMatch(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}

func newOAuthNonce() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func builtInOAuthProviderProfiles() []OAuthProviderProfile {
	return []OAuthProviderProfile{
		{
			ID: "openai-codex", Name: "OpenAI / Codex", Provider: "openai", APIURL: upstream.DefaultXIASSBaseURL,
			APIStyle: "responses", AuthMode: "bearer", Available: oauthProfileReady, AutoLoopback: true,
			Description: "公开客户端的 OAuth 2.0 + PKCE 登录，使用本机回调。",
			OAuth: storage.OAuthConfiguration{
				AuthorizationURL: "https://auth.openai.com/oauth/authorize", TokenURL: "https://auth.openai.com/oauth/token",
				ClientID: "app_EMoamEEZ73f0CkXaXp7hrann", RedirectURI: "http://localhost:1455/auth/callback",
				Scopes: "openid profile email offline_access",
			},
			AuthorizationParameters: map[string]string{"id_token_add_organizations": "true", "codex_cli_simplified_flow": "true"},
			RefreshScopes:           "openid profile email",
			PKCEVerifierFormat:      oauthflow.PKCEVerifierFormatOpenAIHex,
			Message:                 "仅包含公开 OAuth 客户端信息；不会嵌入 Client Secret 或账户令牌。",
		},
		{
			ID: "grok-cli", Name: "Grok", Provider: "grok", APIURL: upstream.DefaultXIASSBaseURL,
			APIStyle: "responses", AuthMode: "bearer", Available: oauthProfileReady, AutoLoopback: true,
			Description: "公开客户端的 OAuth 2.0 + PKCE 登录，授权完成后自动回到本机。",
			OAuth: storage.OAuthConfiguration{
				AuthorizationURL: "https://auth.x.ai/oauth2/authorize", TokenURL: "https://auth.x.ai/oauth2/token",
				ClientID: "b1a00492-073a-47ea-816f-4c329264a828", RedirectURI: "http://127.0.0.1:56121/callback",
				Scopes: "openid profile email offline_access grok-cli:access api:access",
			},
			AuthorizationParameters: map[string]string{"plan": "generic"},
			NonceParameter:          "nonce",
			Message:                 "每次授权都会生成一次性 nonce；不会伪造第三方 referrer 或嵌入任何令牌。",
		},
		{
			ID: "claude-code", Name: "Claude Code", Provider: "anthropic", APIURL: upstream.DefaultXIASSBaseURL,
			APIStyle: "messages", AuthMode: "bearer", Available: oauthProfileManual, AutoLoopback: false,
			Description: "公开 PKCE 参数预设。该客户端使用提供方固定回调，因此完成后需要粘贴完整回调。",
			OAuth: storage.OAuthConfiguration{
				AuthorizationURL: "https://claude.ai/oauth/authorize", TokenURL: "https://platform.claude.com/v1/oauth/token",
				ClientID: "9d1c250a-e61b-44d9-88ed-5944d1962f5e", RedirectURI: "https://platform.claude.com/oauth/code/callback",
				Scopes: "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload",
			},
			AuthorizationParameters: map[string]string{"code": "true"},
			Message:                 "该预设不启动本机监听器；可在账户页面改为你已注册的公开客户端和回调。",
		},
		{
			ID: "gemini-google", Name: "Gemini / Google OAuth", Provider: "openai", APIURL: upstream.DefaultXIASSBaseURL,
			APIStyle: "auto", AuthMode: "bearer", Available: oauthProfileNeedsClientID, RequiresClientID: true, AutoLoopback: true,
			Description: "使用你在 Google Cloud 创建的 Desktop OAuth Client，并通过随机本机端口回调。",
			OAuth: storage.OAuthConfiguration{
				AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token",
				RedirectURI: "http://127.0.0.1:0/oauth/callback",
				Scopes:      "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever",
			},
			AuthorizationParameters: map[string]string{"access_type": "offline", "prompt": "consent", "include_granted_scopes": "true"},
			Message:                 "请填入自己的公开 Desktop Client ID，并在 Google Cloud 启用所需 API；不需要也不会保存 Client Secret。",
		},
		{
			ID: "antigravity", Name: "Antigravity", Provider: "openai", APIURL: upstream.DefaultXIASSBaseURL,
			Available:   oauthProfileCustomOnly,
			Description: "此提供方的 XIASS 实现依赖 secret-bound OAuth client，WF 不会复制或嵌入该 Secret。",
			Message:     "请使用 Custom OAuth，并填写你自己注册的公开客户端；无法安全预设 Antigravity 的 secret-bound 流程。",
		},
		{
			ID: "custom", Name: "Custom OAuth", Provider: "custom", APIURL: upstream.DefaultXIASSBaseURL,
			Available:   oauthProfileCustomOnly,
			Description: "保留全部手工 OAuth 配置能力。",
			Message:     "请在账户页面填写授权地址、令牌地址、公开 Client ID、回调地址和 scopes，然后使用原有 OAuth 登录。",
		},
	}
}

func oauthProviderProfile(profileID string) (OAuthProviderProfile, bool) {
	for _, profile := range builtInOAuthProviderProfiles() {
		if profile.ID == strings.TrimSpace(profileID) {
			return cloneOAuthProviderProfile(profile), true
		}
	}
	return OAuthProviderProfile{}, false
}

func cloneOAuthProviderProfile(profile OAuthProviderProfile) OAuthProviderProfile {
	profile.AuthorizationParameters = cloneOAuthParameters(profile.AuthorizationParameters)
	return profile
}

func cloneOAuthParameters(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

type oauthCompletionEvent struct {
	OK        bool                    `json:"ok"`
	Message   string                  `json:"message"`
	SessionID string                  `json:"sessionId"`
	AccountID string                  `json:"accountId,omitempty"`
	Identity  oauthCompletionIdentity `json:"identity,omitempty"`
}

type oauthCompletionIdentity struct {
	Email   string `json:"email,omitempty"`
	Subject string `json:"subject,omitempty"`
	Plan    string `json:"plan,omitempty"`
}

func redactOAuthCompletionEvent(sessionID string, result OAuthCompletionResult) oauthCompletionEvent {
	return oauthCompletionEvent{
		OK:        result.OK,
		Message:   result.Message,
		SessionID: sessionID,
		AccountID: result.AccountID,
		Identity: oauthCompletionIdentity{
			Email:   result.Identity.Email,
			Subject: result.Identity.Subject,
			Plan:    result.Identity.Plan,
		},
	}
}

func applyOAuthProviderProfile(draft storage.UpstreamAccount, profile OAuthProviderProfile) storage.UpstreamAccount {
	draft.Type = "oauth"
	if strings.TrimSpace(profile.Name) != "" && strings.TrimSpace(draft.Name) == "" {
		draft.Name = profile.Name + " 账户"
	}
	if strings.TrimSpace(profile.Provider) != "" {
		draft.Provider = profile.Provider
	}
	if strings.TrimSpace(draft.APIURL) == "" {
		draft.APIURL = profile.APIURL
	}
	if strings.TrimSpace(profile.APIStyle) != "" {
		draft.APIStyle = profile.APIStyle
	}
	if strings.TrimSpace(profile.AuthMode) != "" {
		draft.AuthMode = profile.AuthMode
	}
	oauthDefaults := profile.OAuth
	if strings.TrimSpace(oauthDefaults.RefreshScopes) == "" {
		oauthDefaults.RefreshScopes = profile.RefreshScopes
	}
	draft.OAuth = mergeOAuthProfileConfiguration(draft.OAuth, oauthDefaults, profile.AutoLoopback)
	return draft
}

func mergeOAuthProfileConfiguration(current, defaults storage.OAuthConfiguration, autoLoopback bool) storage.OAuthConfiguration {
	if strings.TrimSpace(current.AuthorizationURL) == "" {
		current.AuthorizationURL = defaults.AuthorizationURL
	}
	if strings.TrimSpace(current.TokenURL) == "" {
		current.TokenURL = defaults.TokenURL
	}
	if strings.TrimSpace(current.ClientID) == "" {
		current.ClientID = defaults.ClientID
	}
	if strings.TrimSpace(current.RedirectURI) == "" || (autoLoopback && strings.EqualFold(strings.TrimSpace(current.RedirectURI), defaultCustomOAuthRedirectURI)) {
		current.RedirectURI = defaults.RedirectURI
	}
	if strings.TrimSpace(current.Scopes) == "" || strings.EqualFold(strings.Join(strings.Fields(current.Scopes), " "), "openid profile email offline_access") {
		current.Scopes = defaults.Scopes
	}
	if strings.TrimSpace(current.RefreshScopes) == "" {
		current.RefreshScopes = defaults.RefreshScopes
	}
	return current
}

func redactOAuthDraft(account storage.UpstreamAccount) storage.UpstreamAccount {
	account.APIKey = ""
	account.Credentials = nil
	return account
}
