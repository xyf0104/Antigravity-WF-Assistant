package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"antigravity-byok/internal/storage"
)

func TestImportOAuthRefreshTokenExchangesAndStoresOAuthAccount(t *testing.T) {
	storage.Init(t.TempDir())
	const refreshToken = "mobile-refresh-token"
	var received url.Values
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/token" {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodPost {
			t.Errorf("token method = %s, want POST", request.Method)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		received, err = url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"exchanged-access-token","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	app := &App{}
	result := app.ImportOAuthRefreshToken(storage.UpstreamAccount{
		Name: "Mobile RT", Provider: "openai", Type: "refresh_token", APIURL: tokenServer.URL,
		AuthMode: "bearer", Enabled: true, MaxConcurrency: 1,
		OAuth: storage.OAuthConfiguration{
			AuthorizationURL: tokenServer.URL + "/authorize",
			TokenURL:         tokenServer.URL + "/token",
			ClientID:         "public-client-id",
			RedirectURI:      tokenServer.URL + "/callback",
			Scopes:           "openid offline_access",
		},
	}, refreshToken)
	if !result.OK || result.AccountID == "" {
		t.Fatalf("import result = %+v", result)
	}
	if strings.Contains(result.Message, refreshToken) {
		t.Fatalf("result leaked refresh token: %+v", result)
	}
	if got, want := received.Get("grant_type"), "refresh_token"; got != want {
		t.Fatalf("grant_type = %q, want %q", got, want)
	}
	if got, want := received.Get("refresh_token"), refreshToken; got != want {
		t.Fatalf("refresh_token form field = %q, want input token", got)
	}
	if got, want := received.Get("client_id"), "public-client-id"; got != want {
		t.Fatalf("client_id = %q, want %q", got, want)
	}
	if got, want := received.Get("scope"), "openid offline_access"; got != want {
		t.Fatalf("scope = %q, want %q", got, want)
	}

	account, err := storage.GetUpstreamAccount(result.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Type != "oauth" || account.APIKey != "exchanged-access-token" || account.EffectiveAPIKey() != "exchanged-access-token" {
		t.Fatalf("refresh token was not converted to an OAuth access credential: %+v", account)
	}
	if got := credentialText(account.Credentials, "refresh_token"); got != refreshToken {
		t.Fatalf("stored refresh token = %q, want original token", got)
	}
	if account.AuthExpiresAt == "" || account.Identity.Source != "Refresh Token / Mobile RT 导入" {
		t.Fatalf("OAuth metadata was not retained: %+v", account)
	}
}

func TestCompleteOAuthAuthorizationAssignsNewAccountIDAndConsumesSession(t *testing.T) {
	storage.Init(t.TempDir())
	var received url.Values
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/token" {
			http.NotFound(writer, request)
			return
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		received = request.PostForm
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"authorization-access-token","refresh_token":"authorization-refresh-token","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	app := &App{oauthSessions: make(map[string]*pendingOAuthSession)}
	started := app.StartOAuthAuthorization(storage.UpstreamAccount{
		Name: "OAuth browser login", Provider: "openai", Type: "oauth", APIURL: tokenServer.URL,
		AuthMode: "bearer", Enabled: true, MaxConcurrency: 1,
		OAuth: storage.OAuthConfiguration{
			AuthorizationURL: tokenServer.URL + "/authorize",
			TokenURL:         tokenServer.URL + "/token",
			ClientID:         "public-client-id",
			RedirectURI:      tokenServer.URL + "/callback",
			Scopes:           "openid profile",
		},
	})
	if !started.OK || started.SessionID == "" {
		t.Fatalf("start OAuth result = %+v", started)
	}

	completed := app.CompleteOAuthAuthorization(started.SessionID, "authorization-code")
	if !completed.OK || completed.AccountID == "" {
		t.Fatalf("complete OAuth result = %+v", completed)
	}
	if got, want := received.Get("grant_type"), "authorization_code"; got != want {
		t.Fatalf("grant_type = %q, want %q", got, want)
	}
	if got, want := received.Get("client_id"), "public-client-id"; got != want {
		t.Fatalf("client_id = %q, want %q", got, want)
	}
	if got, want := received.Get("code"), "authorization-code"; got != want {
		t.Fatalf("code = %q, want %q", got, want)
	}

	account, err := storage.GetUpstreamAccount(completed.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.APIKey != "authorization-access-token" || credentialText(account.Credentials, "refresh_token") != "authorization-refresh-token" {
		t.Fatalf("OAuth credentials were not stored: %+v", account)
	}
	app.oauthMu.Lock()
	_, retained := app.oauthSessions[started.SessionID]
	app.oauthMu.Unlock()
	if retained {
		t.Fatal("successful OAuth authorization retained its one-time session")
	}
}

func TestImportOAuthRefreshTokenRejectsEmptyToken(t *testing.T) {
	storage.Init(t.TempDir())
	result := (&App{}).ImportOAuthRefreshToken(storage.UpstreamAccount{}, " \t ")
	if result.OK || !strings.Contains(result.Message, "Refresh Token") {
		t.Fatalf("empty refresh token result = %+v", result)
	}
}

func TestSaveUpstreamAccountRejectsRawRefreshToken(t *testing.T) {
	storage.Init(t.TempDir())
	result := (&App{}).SaveUpstreamAccount(storage.UpstreamAccount{
		Provider: "openai", Type: "refresh_token", APIURL: "https://api.example.test", APIKey: "raw-refresh-token",
	})
	if result.OK || !strings.Contains(result.Message, "兑换") {
		t.Fatalf("raw refresh token save result = %+v", result)
	}
}
