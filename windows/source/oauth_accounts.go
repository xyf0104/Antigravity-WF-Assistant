package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"antigravity-wf-assistant/internal/oauthflow"
	"antigravity-wf-assistant/internal/stats"
	"antigravity-wf-assistant/internal/storage"
	"antigravity-wf-assistant/internal/upstream"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// pendingOAuthSession binds a short-lived PKCE session to the account draft
// entered in the renderer. Secrets never leave this Go-side structure.
type pendingOAuthSession struct {
	flow         *oauthflow.Flow
	account      storage.UpstreamAccount
	state        string
	expires      time.Time
	completionMu sync.Mutex
}

type OAuthAuthorizationResult struct {
	OK                       bool   `json:"ok"`
	Message                  string `json:"message"`
	SessionID                string `json:"sessionId,omitempty"`
	AuthorizationURL         string `json:"authorizationUrl,omitempty"`
	RedirectURI              string `json:"redirectUri,omitempty"`
	ProfileID                string `json:"profileId,omitempty"`
	AutomaticCallback        bool   `json:"automaticCallback"`
	ManualCompletionRequired bool   `json:"manualCompletionRequired"`
	ExpiresAt                string `json:"expiresAt,omitempty"`
}

type OAuthCompletionResult struct {
	OK        bool                    `json:"ok"`
	Message   string                  `json:"message"`
	AccountID string                  `json:"accountId,omitempty"`
	Identity  storage.AccountIdentity `json:"identity,omitempty"`
}

// UpstreamAccountView is the redacted account data sent to the renderer.
// LocalUsage comes solely from WF's proxy trace; it is not a provider billing
// estimate and is deliberately kept out of upstream_accounts.json.
type UpstreamAccountView struct {
	storage.UpstreamAccount
	LocalUsage        stats.AccountUsage `json:"localUsage"`
	HasPrivateHeaders bool               `json:"hasPrivateHeaders"`
}

// StartOAuthAuthorization creates a short-lived, provider-neutral PKCE
// session and opens its one-time authorization URL in the system browser. The
// account owner supplies a registered public OAuth client configuration; WF
// deliberately does not ship a third-party client ID or secret.
func (a *App) StartOAuthAuthorization(draft storage.UpstreamAccount) OAuthAuthorizationResult {
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

	flow, err := oauthflow.New(oauthflow.Config{
		AuthorizationURL: draft.OAuth.AuthorizationURL,
		TokenURL:         draft.OAuth.TokenURL,
		PublicClientID:   draft.OAuth.ClientID,
		RedirectURI:      draft.OAuth.RedirectURI,
		Scopes:           strings.Fields(draft.OAuth.Scopes),
		RefreshScopes:    strings.Fields(draft.OAuth.RefreshScopes),
	})
	if err != nil {
		return OAuthAuthorizationResult{Message: oauthErrorMessage(err)}
	}
	upstreamConfig := upstream.ConfigFromAccount(draft)
	upstreamConfig.APIKey = "oauth-pending"
	if err := upstream.ValidateConfig(upstreamConfig); err != nil {
		return OAuthAuthorizationResult{Message: err.Error()}
	}
	authorization, err := flow.Begin()
	if err != nil {
		return OAuthAuthorizationResult{Message: oauthErrorMessage(err)}
	}

	a.oauthMu.Lock()
	a.ensureOAuthMapsLocked()
	a.discardExpiredOAuthSessionsLocked(time.Now())
	a.oauthSessions[authorization.SessionID] = &pendingOAuthSession{
		flow: flow, account: draft, state: authorization.State, expires: authorization.ExpiresAt,
	}
	a.oauthMu.Unlock()
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, authorization.URL)
	}
	return OAuthAuthorizationResult{
		OK:                       true,
		Message:                  "已在浏览器打开授权页；授权后请粘贴完整回调 URL 或授权码。",
		SessionID:                authorization.SessionID,
		AuthorizationURL:         authorization.URL,
		RedirectURI:              draft.OAuth.RedirectURI,
		ManualCompletionRequired: true,
		ExpiresAt:                authorization.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

// CompleteOAuthAuthorization exchanges a copied callback URL or code. A raw
// code is bound to the retained state from StartOAuthAuthorization, while a
// complete callback has its state verified before any token is accepted.
func (a *App) CompleteOAuthAuthorization(sessionID, callback string) OAuthCompletionResult {
	sessionID = strings.TrimSpace(sessionID)
	a.oauthMu.Lock()
	a.ensureOAuthMapsLocked()
	a.discardExpiredOAuthSessionsLocked(time.Now())
	if record, found := a.oauthResults[sessionID]; found && record.result.OK {
		a.oauthMu.Unlock()
		return record.result
	}
	session := a.oauthSessions[sessionID]
	a.oauthMu.Unlock()
	if session == nil {
		return OAuthCompletionResult{Message: "授权会话不存在或已过期，请重新生成登录链接。"}
	}
	// A manual fallback and a loopback redirect can arrive at nearly the same
	// time. Serialising them at the session (not HTTP listener) level lets both
	// paths safely converge on one persisted account and one completion result.
	session.completionMu.Lock()
	defer session.completionMu.Unlock()

	a.oauthMu.Lock()
	a.discardExpiredOAuthSessionsLocked(time.Now())
	if record, found := a.oauthResults[sessionID]; found && record.result.OK {
		a.oauthMu.Unlock()
		return record.result
	}
	if a.oauthSessions[sessionID] != session {
		a.oauthMu.Unlock()
		return OAuthCompletionResult{Message: "授权会话不存在或已过期，请重新生成登录链接。"}
	}
	a.oauthMu.Unlock()

	parsed, err := oauthflow.ExtractCallback(callback)
	if err != nil {
		return OAuthCompletionResult{Message: oauthErrorMessage(err)}
	}
	if strings.TrimSpace(parsed.State) == "" {
		parsed.State = session.state
	}
	ctx, cancel := a.upstreamContext(45 * time.Second)
	defer cancel()
	token, err := session.flow.ExchangeCode(ctx, sessionID, parsed.Code, parsed.State)
	if err != nil {
		return OAuthCompletionResult{Message: oauthErrorMessage(err)}
	}

	account := applyOAuthToken(session.account, token, "OAuth 授权响应")
	result := a.SaveUpstreamAccount(account)
	if !result.OK {
		return OAuthCompletionResult{Message: result.Message}
	}
	stored, err := storage.GetUpstreamAccount(account.ID)
	completion := OAuthCompletionResult{
		OK: true, Message: "OAuth 授权已完成，账户已加入本地账户池。", AccountID: account.ID,
	}
	if err != nil {
		completion.Message = result.Message
	} else {
		completion.Identity = stored.Identity
	}
	a.recordOAuthAuthorizationResult(sessionID, completion)
	a.oauthMu.Lock()
	delete(a.oauthSessions, sessionID)
	a.oauthMu.Unlock()
	return completion
}

// ImportOAuthRefreshToken exchanges a user-supplied refresh token with the
// configured public OAuth client, then stores only the exchanged access token
// and refresh-token credential as an OAuth account. The refresh token is never
// treated as an upstream API key or returned to the renderer.
func (a *App) ImportOAuthRefreshToken(draft storage.UpstreamAccount, refreshToken string) OAuthCompletionResult {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return OAuthCompletionResult{Message: "请填写 Refresh Token 或 Mobile RT。"}
	}
	if strings.TrimSpace(draft.ID) == "" {
		accountID, err := newOAuthImportedAccountID()
		if err != nil {
			return OAuthCompletionResult{Message: "无法创建本地账户标识：" + err.Error()}
		}
		draft.ID = accountID
	} else {
		existing, err := storage.GetUpstreamAccount(draft.ID)
		if err != nil {
			return OAuthCompletionResult{Message: err.Error()}
		}
		// Account views intentionally omit credentials. Retain unrelated existing
		// OAuth metadata while applyOAuthToken replaces the access/refresh pair.
		if draft.Credentials == nil {
			draft.Credentials = existing.Credentials
		}
		draft.Identity = existing.Identity
	}

	// Validate the target inference configuration without ever accepting the
	// refresh token as that configuration's API key.
	upstreamConfig := upstream.ConfigFromAccount(draft)
	upstreamConfig.APIKey = "oauth-pending"
	if err := upstream.ValidateConfig(upstreamConfig); err != nil {
		return OAuthCompletionResult{Message: err.Error()}
	}
	flow, err := oauthflow.New(oauthflow.Config{
		AuthorizationURL: draft.OAuth.AuthorizationURL,
		TokenURL:         draft.OAuth.TokenURL,
		PublicClientID:   draft.OAuth.ClientID,
		RedirectURI:      draft.OAuth.RedirectURI,
		Scopes:           strings.Fields(draft.OAuth.Scopes),
		RefreshScopes:    strings.Fields(draft.OAuth.RefreshScopes),
	})
	if err != nil {
		return OAuthCompletionResult{Message: oauthErrorMessage(err)}
	}
	ctx, cancel := a.upstreamContext(45 * time.Second)
	defer cancel()
	token, err := flow.Refresh(ctx, refreshToken)
	if err != nil {
		return OAuthCompletionResult{Message: oauthErrorMessage(err)}
	}
	account := applyOAuthToken(draft, token, "Refresh Token / Mobile RT 导入")
	if err := storage.SaveUpstreamAccount(account); err != nil {
		return OAuthCompletionResult{Message: err.Error()}
	}
	stored, err := storage.GetUpstreamAccount(account.ID)
	if err != nil {
		return OAuthCompletionResult{OK: true, Message: "刷新令牌已兑换并保存为 OAuth 账户。", AccountID: account.ID}
	}
	return OAuthCompletionResult{
		OK: true, Message: "刷新令牌已兑换并保存为 OAuth 账户。",
		AccountID: stored.ID, Identity: stored.Identity,
	}
}

func newOAuthImportedAccountID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "acct-" + hex.EncodeToString(bytes), nil
}

// RefreshUpstreamOAuthAccount rotates an access token only after the user
// explicitly requests it. The refresh token stays in private local storage.
func (a *App) RefreshUpstreamOAuthAccount(accountID string) Result {
	account, err := storage.GetUpstreamAccount(accountID)
	if err != nil {
		return Result{Message: err.Error()}
	}
	refreshToken := credentialText(account.Credentials, "refresh_token", "refreshToken", "mobile_rt", "mobileRT")
	if refreshToken == "" {
		return Result{Message: "该账户没有可用的刷新令牌，请重新授权或导入完整 OAuth JSON。"}
	}
	flow, err := oauthflow.New(oauthflow.Config{
		AuthorizationURL: account.OAuth.AuthorizationURL,
		TokenURL:         account.OAuth.TokenURL,
		PublicClientID:   account.OAuth.ClientID,
		RedirectURI:      account.OAuth.RedirectURI,
		Scopes:           strings.Fields(account.OAuth.Scopes),
		RefreshScopes:    strings.Fields(account.OAuth.RefreshScopes),
	})
	if err != nil {
		return Result{Message: oauthErrorMessage(err)}
	}
	ctx, cancel := a.upstreamContext(45 * time.Second)
	defer cancel()
	token, err := flow.Refresh(ctx, refreshToken)
	if err != nil {
		return Result{Message: oauthErrorMessage(err)}
	}
	account = applyOAuthToken(account, token, "OAuth 刷新响应")
	if err := storage.SaveUpstreamAccount(account); err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true, Message: "访问令牌已刷新"}
}

// RefreshUpstreamAccountQuota calls an optional, provider-documented quota
// endpoint configured by the account owner. It never runs on page load.
func (a *App) RefreshUpstreamAccountQuota(accountID string) upstream.QuotaResult {
	account, err := storage.GetUpstreamAccount(accountID)
	if err != nil {
		return upstream.QuotaResult{Message: err.Error()}
	}
	ctx, cancel := a.upstreamContext(30 * time.Second)
	defer cancel()
	account, err = refreshExplicitUpstreamAccount(ctx, account)
	if err != nil {
		return upstream.QuotaResult{Message: err.Error()}
	}
	result := upstream.FetchQuota(ctx, upstream.ConfigFromAccount(account), account.QuotaURL)
	if result.OK {
		if err := storage.SaveQuotaSnapshot(account.ID, result.Snapshot); err != nil {
			result.OK = false
			result.Message = err.Error()
		}
	}
	return result
}

func (a *App) discardExpiredOAuthSessionsLocked(now time.Time) {
	for sessionID, session := range a.oauthSessions {
		if session == nil || !now.Before(session.expires) {
			delete(a.oauthSessions, sessionID)
			if callback := a.oauthLoopbacks[sessionID]; callback != nil {
				callback.Close()
				delete(a.oauthLoopbacks, sessionID)
			}
		}
	}
	for sessionID, record := range a.oauthResults {
		if !now.Before(record.expires) {
			delete(a.oauthResults, sessionID)
		}
	}
}

func applyOAuthToken(account storage.UpstreamAccount, token oauthflow.Token, source string) storage.UpstreamAccount {
	credentials := make(map[string]any, len(account.Credentials)+7)
	for key, value := range account.Credentials {
		credentials[key] = value
	}
	credentials["access_token"] = token.AccessToken
	if token.RefreshToken != "" {
		credentials["refresh_token"] = token.RefreshToken
	}
	if token.IDToken != "" {
		credentials["id_token"] = token.IDToken
	}
	if token.TokenType != "" {
		credentials["token_type"] = token.TokenType
	}
	if token.Scope != "" {
		credentials["scope"] = token.Scope
	}
	// Keep the public OAuth client beside the rotated tokens. This mirrors
	// XIASS exports and lets a later JSON re-import refresh with the exact
	// registered client rather than guessing one. It is not a client secret.
	if clientID := strings.TrimSpace(account.OAuth.ClientID); clientID != "" {
		credentials["client_id"] = clientID
	}
	account.Type = "oauth"
	account.APIKey = token.AccessToken
	account.Credentials = credentials
	if token.ExpiresAt.IsZero() {
		account.AuthExpiresAt = ""
	} else {
		account.AuthExpiresAt = token.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if account.AuthMode == "" {
		account.AuthMode = "bearer"
	}
	account.Identity.Source = source
	account.Identity.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return account
}

func credentialText(credentials map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := credentials[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func oauthErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, oauthflow.ErrInvalidConfig):
		return "OAuth 配置无效：请检查授权地址、令牌地址、公开客户端 ID 和已注册的回调地址。"
	case errors.Is(err, oauthflow.ErrAuthorizationDenied):
		return "授权被取消或拒绝。"
	case errors.Is(err, oauthflow.ErrInvalidCallback):
		return "回调地址或授权码无效。"
	case errors.Is(err, oauthflow.ErrStateMismatch):
		return "回调状态不匹配，请重新生成登录链接。"
	case errors.Is(err, oauthflow.ErrSessionNotFound):
		return "授权会话不存在或已过期，请重新生成登录链接。"
	case errors.Is(err, oauthflow.ErrTokenExchange):
		return "令牌兑换失败，请检查 OAuth 配置、授权码和网络后重试。"
	default:
		return "OAuth 操作失败：" + err.Error()
	}
}
