package storage

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func importedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal JWT claims: %v", err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func importAccountJSON(t *testing.T, value map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal imported account: %v", err)
	}
	return string(raw)
}

func TestImportUpstreamAccountsReadsNestedXIASSOAuthTokens(t *testing.T) {
	Init(t.TempDir())
	expiresAt := time.Now().UTC().Add(90 * time.Minute).Truncate(time.Second)
	idToken := importedJWT(t, map[string]any{"sub": "jwt-subject"})

	result := ImportUpstreamAccounts(importAccountJSON(t, map[string]any{
		"name":     "XIASS nested OAuth",
		"platform": "openai",
		"type":     "oauth",
		"api_url":  "https://api.xiass.example",
		"credentials": map[string]any{
			"auth": map[string]any{
				"tokens": map[string]any{
					"access_token":  "nested-access-token",
					"refresh_token": "nested-refresh-token",
					"id_token":      idToken,
					"expires_at":    expiresAt.Unix(),
				},
			},
		},
		"extra": map[string]any{
			"email":           "xiass@example.test",
			"plan":            "plus",
			"organization_id": "org-xiass",
			"privacy_mode":    "restricted",
		},
		"oauth": map[string]any{
			"authorization_url": "https://login.example.test/authorize",
			"token_url":         "https://login.example.test/token",
			"client_id":         "imported-public-client",
			"redirect_uri":      "http://127.0.0.1:1455/callback",
			"scopes":            []any{"openid", "offline_access"},
		},
	}))
	if !result.OK || result.Added != 1 {
		t.Fatalf("import result = %#v, want one imported account", result)
	}

	accounts, err := LoadUpstreamAccounts()
	if err != nil || len(accounts) != 1 {
		t.Fatalf("load accounts = %#v, %v", accounts, err)
	}
	account := accounts[0]
	if account.EffectiveAPIKey() != "nested-access-token" {
		t.Fatalf("effective API key = %q, want nested access token", account.EffectiveAPIKey())
	}
	for key, want := range map[string]string{
		"access_token":  "nested-access-token",
		"refresh_token": "nested-refresh-token",
		"id_token":      idToken,
	} {
		if got, _ := account.Credentials[key].(string); got != want {
			t.Fatalf("canonical credential %s = %q, want %q", key, got, want)
		}
	}
	if account.AuthExpiresAt != expiresAt.Format(time.RFC3339) {
		t.Fatalf("auth expiry = %q, want %q", account.AuthExpiresAt, expiresAt.Format(time.RFC3339))
	}
	if got, want := account.Identity.Email, "xiass@example.test"; got != want {
		t.Fatalf("identity email = %q, want %q", got, want)
	}
	if got, want := account.Identity.Plan, "plus"; got != want {
		t.Fatalf("identity plan = %q, want %q", got, want)
	}
	if got, want := account.Identity.OrganizationID, "org-xiass"; got != want {
		t.Fatalf("identity organization = %q, want %q", got, want)
	}
	if account.OAuth != (OAuthConfiguration{
		AuthorizationURL: "https://login.example.test/authorize",
		TokenURL:         "https://login.example.test/token",
		ClientID:         "imported-public-client",
		RedirectURI:      "http://127.0.0.1:1455/callback",
		Scopes:           "openid offline_access",
	}) {
		t.Fatalf("OAuth configuration = %#v, want explicitly imported public config", account.OAuth)
	}
}

func TestImportUpstreamAccountsReadsCodexAuthJSONWithoutDefaultOAuthClient(t *testing.T) {
	Init(t.TempDir())
	expiresAt := time.Now().UTC().Add(45 * time.Minute).Truncate(time.Second)
	idToken := importedJWT(t, map[string]any{
		"sub":   "codex-user",
		"email": "codex@example.test",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type":  "pro",
			"chatgpt_account_id": "chatgpt-account",
		},
	})

	result := ImportUpstreamAccounts(importAccountJSON(t, map[string]any{
		"OPENAI_API_KEY": nil,
		"tokens": map[string]any{
			"access_token":  "codex-access-token",
			"refresh_token": "codex-refresh-token",
			"id_token":      idToken,
			"expires_at":    float64(expiresAt.Unix()),
		},
	}))
	if !result.OK || result.Added != 1 {
		t.Fatalf("import result = %#v, want one imported account", result)
	}

	accounts, err := LoadUpstreamAccounts()
	if err != nil || len(accounts) != 1 {
		t.Fatalf("load accounts = %#v, %v", accounts, err)
	}
	account := accounts[0]
	if account.Type != "oauth" {
		t.Fatalf("account type = %q, want oauth", account.Type)
	}
	if account.EffectiveAPIKey() != "codex-access-token" {
		t.Fatalf("effective API key = %q, want Codex access token", account.EffectiveAPIKey())
	}
	if account.AuthExpiresAt != expiresAt.Format(time.RFC3339) {
		t.Fatalf("auth expiry = %q, want %q", account.AuthExpiresAt, expiresAt.Format(time.RFC3339))
	}
	if account.Identity.Email != "codex@example.test" || account.Identity.Subject != "codex-user" ||
		account.Identity.Plan != "pro" || account.Identity.OrganizationID != "chatgpt-account" {
		t.Fatalf("identity = %#v, want Codex token claims", account.Identity)
	}
	if account.OAuth != (OAuthConfiguration{}) {
		t.Fatalf("OAuth config must stay empty when JSON did not provide one: %#v", account.OAuth)
	}
}

func TestEffectiveAPIKeyUsesOnlyBoundedCredentialContainers(t *testing.T) {
	withinLimit := UpstreamAccount{Credentials: map[string]any{
		"auth": map[string]any{
			"credentials": map[string]any{
				"tokens": map[string]any{"access_token": "within-limit"},
			},
		},
	}}
	if got := withinLimit.EffectiveAPIKey(); got != "within-limit" {
		t.Fatalf("nested effective API key = %q, want allowed-depth token", got)
	}

	beyondLimit := UpstreamAccount{Credentials: map[string]any{
		"auth": map[string]any{
			"credentials": map[string]any{
				"tokens": map[string]any{
					"auth": map[string]any{"access_token": "must-not-be-scanned"},
				},
			},
		},
	}}
	if got := beyondLimit.EffectiveAPIKey(); got != "" {
		t.Fatalf("effective API key scanned beyond bounded credential depth: %q", got)
	}
}
