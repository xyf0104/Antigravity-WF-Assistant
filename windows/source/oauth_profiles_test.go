package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"antigravity-byok/internal/oauthflow"
	"antigravity-byok/internal/storage"
)

func TestOAuthProfilesArePublicAndPreserveUserOAuthOverrides(t *testing.T) {
	app := &App{}
	profiles := app.GetOAuthProviderProfiles()
	if len(profiles) == 0 {
		t.Fatal("expected built-in OAuth profiles")
	}

	encoded, err := json.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"access_token", "refresh_token", "client_secret", "\"apiKey\"", "\"credentials\""} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("OAuth profile payload must not expose %q", forbidden)
		}
	}

	// The profile list must be cloned: modifying data returned to one renderer
	// must not mutate the presets observed by another renderer.
	profiles[0].AuthorizationParameters["mutated"] = "true"
	for _, profile := range app.GetOAuthProviderProfiles() {
		if profile.AuthorizationParameters["mutated"] != "" {
			t.Fatal("OAuth profile presets leaked a mutable authorization-parameter map")
		}
	}

	draft := storage.UpstreamAccount{
		APIURL: "https://gateway.example.test/v1",
		APIKey: "must-not-return-to-renderer",
		Credentials: map[string]any{
			"refresh_token": "must-not-return-to-renderer",
		},
		OAuth: storage.OAuthConfiguration{
			AuthorizationURL: "https://custom.example.test/authorize",
			TokenURL:         "https://custom.example.test/token",
			ClientID:         "my-public-client-id",
			RedirectURI:      "http://127.0.0.1:39001/callback",
			Scopes:           "custom scope",
		},
	}
	applied := app.ApplyOAuthProviderProfile("openai-codex", draft)
	if !applied.OK {
		t.Fatalf("apply profile result = %+v", applied)
	}
	if applied.Account.APIKey != "" || applied.Account.Credentials != nil {
		t.Fatal("applied OAuth draft leaked existing account credentials")
	}
	if got, want := applied.Account.OAuth.AuthorizationURL, draft.OAuth.AuthorizationURL; got != want {
		t.Fatalf("authorization URL override = %q, want %q", got, want)
	}
	if got, want := applied.Account.OAuth.TokenURL, draft.OAuth.TokenURL; got != want {
		t.Fatalf("token URL override = %q, want %q", got, want)
	}
	if got, want := applied.Account.OAuth.ClientID, draft.OAuth.ClientID; got != want {
		t.Fatalf("client ID override = %q, want %q", got, want)
	}
	if got, want := applied.Account.OAuth.RedirectURI, draft.OAuth.RedirectURI; got != want {
		t.Fatalf("redirect URI override = %q, want %q", got, want)
	}
	if got, want := applied.Account.OAuth.Scopes, draft.OAuth.Scopes; got != want {
		t.Fatalf("scope override = %q, want %q", got, want)
	}
	if got, want := applied.Account.OAuth.RefreshScopes, "openid profile email"; got != want {
		t.Fatalf("profile refresh scopes = %q, want %q", got, want)
	}
}

func TestProfileAuthorizationUsesPKCEAndSafeProviderParameters(t *testing.T) {
	storage.Init(t.TempDir())
	app := &App{}
	started := app.BeginOAuthProviderAuthorization("openai-codex", storage.UpstreamAccount{
		APIURL: "https://gateway.example.test/v1",
		OAuth:  storage.OAuthConfiguration{RedirectURI: "http://127.0.0.1:0/callback"},
	})
	defer app.stopOAuthLoopbacks()
	if !started.OK || started.SessionID == "" || started.AuthorizationURL == "" {
		t.Fatalf("begin profile OAuth result = %+v", started)
	}
	if started.RedirectURI == "" || !strings.HasPrefix(started.RedirectURI, "http://127.0.0.1:") {
		t.Fatalf("loopback redirect URI = %q, want an allocated local port", started.RedirectURI)
	}

	parsed, err := url.Parse(started.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if got, want := query.Get("id_token_add_organizations"), "true"; got != want {
		t.Fatalf("OpenAI authorization parameter = %q, want %q", got, want)
	}
	if got, want := query.Get("codex_cli_simplified_flow"), "true"; got != want {
		t.Fatalf("Codex authorization parameter = %q, want %q", got, want)
	}
	if query.Get("state") == "" || query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL omitted required PKCE data: %s", started.AuthorizationURL)
	}
	if query.Get("code_verifier") != "" {
		t.Fatalf("authorization URL leaked PKCE verifier: %s", started.AuthorizationURL)
	}

	statusJSON, err := json.Marshal(app.GetOAuthAuthorizationStatus(started.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"access_token", "refresh_token", "code_verifier"} {
		if strings.Contains(string(statusJSON), forbidden) {
			t.Fatalf("pending OAuth status leaked %q", forbidden)
		}
	}
}

func TestOpenAICodexProfileUsesReviewedHexPKCEFormat(t *testing.T) {
	profile, found := oauthProviderProfile("openai-codex")
	if !found {
		t.Fatal("OpenAI Codex profile was not found")
	}
	if got, want := profile.PKCEVerifierFormat, oauthflow.PKCEVerifierFormatOpenAIHex; got != want {
		t.Fatalf("OpenAI profile verifier format = %q, want %q", got, want)
	}
	if got, want := profile.APIURL, storage.OpenAICodexResponsesURL; got != want {
		t.Fatalf("OpenAI profile endpoint = %q, want direct Codex route %q", got, want)
	}
	if profile.EndpointMode != "manual" || profile.APIStyle != "responses" || profile.OAuth.Upstream != storage.OpenAICodexOAuthUpstream {
		t.Fatalf("OpenAI profile direct transport = %#v", profile)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "openai_hex") {
		t.Fatalf("native PKCE format must not be renderer-controlled: %s", encoded)
	}
}

func TestProfileAllowsManualCompletionAfterExternalRedirectOverride(t *testing.T) {
	storage.Init(t.TempDir())
	app := &App{}
	started := app.BeginOAuthProviderAuthorization("openai-codex", storage.UpstreamAccount{
		APIURL: "https://gateway.example.test/v1",
		OAuth:  storage.OAuthConfiguration{RedirectURI: "https://desktop.example.test/oauth/callback"},
	})
	defer app.stopOAuthLoopbacks()
	if !started.OK {
		t.Fatalf("begin profile OAuth with external redirect = %+v", started)
	}
	if started.AutomaticCallback || !started.ManualCompletionRequired {
		t.Fatalf("external redirect completion mode = %+v, want manual", started)
	}
	if got, want := started.RedirectURI, "https://desktop.example.test/oauth/callback"; got != want {
		t.Fatalf("external redirect URI = %q, want %q", got, want)
	}
	app.oauthMu.Lock()
	callback := app.oauthLoopbacks[started.SessionID]
	app.oauthMu.Unlock()
	if callback != nil {
		t.Fatal("external redirect unexpectedly started a local loopback listener")
	}
}

func TestProfileLoopbackPortCollisionFallsBackToManualCompletion(t *testing.T) {
	storage.Init(t.TempDir())
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	redirectURI := "http://" + occupied.Addr().String() + "/callback"
	profile := OAuthProviderProfile{
		ID: "test-loopback-collision", Name: "Test", Provider: "openai", APIURL: "https://gateway.example.test",
		APIStyle: "responses", AuthMode: "bearer", AutoLoopback: true,
		OAuth: storage.OAuthConfiguration{
			AuthorizationURL: "https://auth.example.test/authorize", TokenURL: "https://auth.example.test/token",
			ClientID: "public-client", RedirectURI: redirectURI, Scopes: "openid",
		},
	}
	app := &App{}
	started := app.beginProfileOAuthAuthorization(profile, storage.UpstreamAccount{
		Provider: "openai", Type: "oauth", APIURL: "https://gateway.example.test", APIStyle: "responses", AuthMode: "bearer",
		OAuth: profile.OAuth,
	})
	if !started.OK {
		t.Fatalf("port-collision login must still start: %+v", started)
	}
	if started.AutomaticCallback || !started.ManualCompletionRequired {
		t.Fatalf("port-collision completion mode = %+v, want manual fallback", started)
	}
	if got, want := started.RedirectURI, redirectURI; got != want {
		t.Fatalf("fallback redirect URI = %q, want original registered URI %q", got, want)
	}
	if !strings.Contains(started.Message, "端口被占用") {
		t.Fatalf("fallback message = %q, want clear port-collision guidance", started.Message)
	}
	app.oauthMu.Lock()
	callback := app.oauthLoopbacks[started.SessionID]
	app.oauthMu.Unlock()
	if callback != nil {
		t.Fatal("port-collision fallback unexpectedly retained a loopback listener")
	}
}

func TestLoopbackOAuthCallbackExchangesOnlyMatchingStateAndRedactsStatus(t *testing.T) {
	storage.Init(t.TempDir())
	var tokenCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		tokenCalls.Add(1)
		if err := request.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if got, want := request.PostForm.Get("code"), "good-code"; got != want {
			t.Errorf("authorization code = %q, want %q", got, want)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"secret-access-token","refresh_token":"secret-refresh-token","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	flow, err := oauthflow.New(oauthflow.Config{
		AuthorizationURL: tokenServer.URL + "/authorize",
		TokenURL:         tokenServer.URL + "/token",
		PublicClientID:   "test-public-client",
		RedirectURI:      "http://127.0.0.1:39002/callback",
		Scopes:           []string{"openid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := flow.Begin()
	if err != nil {
		t.Fatal(err)
	}

	app := &App{
		oauthSessions: map[string]*pendingOAuthSession{
			authorization.SessionID: {
				flow: flow,
				account: storage.UpstreamAccount{
					ID: "loopback-account", Name: "loopback", Provider: "openai", Type: "oauth",
					APIURL: tokenServer.URL, APIStyle: "responses", AuthMode: "bearer", Enabled: true,
					MaxConcurrency: 1,
					OAuth: storage.OAuthConfiguration{
						AuthorizationURL: tokenServer.URL + "/authorize", TokenURL: tokenServer.URL + "/token",
						ClientID: "test-public-client", RedirectURI: "http://127.0.0.1:39002/callback", Scopes: "openid",
					},
				},
				state: authorization.State, expires: time.Now().Add(time.Minute),
			},
		},
		oauthResults:   make(map[string]oauthAuthorizationRecord),
		oauthLoopbacks: make(map[string]*oauthLoopbackListener),
	}

	handler := app.loopbackOAuthCallbackHandler(authorization.SessionID, &oauthLoopbackListener{path: "/callback"})
	invalid := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:39002/callback?code=bad-code&state=wrong-state", nil)
	invalid.RemoteAddr = "127.0.0.1:49002"
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if got, want := invalidResponse.Code, http.StatusBadRequest; got != want {
		t.Fatalf("invalid-state callback status = %d, want %d", got, want)
	}
	if got := tokenCalls.Load(); got != 0 {
		t.Fatalf("invalid-state callback called token endpoint %d time(s)", got)
	}

	validURL := "http://127.0.0.1:39002/callback?code=good-code&state=" + url.QueryEscape(authorization.State)
	valid := httptest.NewRequest(http.MethodGet, validURL, nil)
	valid.RemoteAddr = "127.0.0.1:49002"
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, valid)
	if got, want := validResponse.Code, http.StatusOK; got != want {
		t.Fatalf("valid loopback callback status = %d, want %d: %s", got, want, validResponse.Body.String())
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("valid loopback callback called token endpoint %d time(s), want 1", got)
	}

	duplicateResponse := httptest.NewRecorder()
	handler.ServeHTTP(duplicateResponse, valid)
	if got, want := duplicateResponse.Code, http.StatusOK; got != want {
		t.Fatalf("duplicate callback status = %d, want %d: %s", got, want, duplicateResponse.Body.String())
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("duplicate callback exchanged token %d time(s), want exactly one", got)
	}

	stored, err := storage.GetUpstreamAccount("loopback-account")
	if err != nil {
		t.Fatal(err)
	}
	if stored.EffectiveAPIKey() != "secret-access-token" || credentialText(stored.Credentials, "refresh_token") != "secret-refresh-token" {
		t.Fatal("OAuth callback did not persist exchanged credentials")
	}
	statusJSON, err := json.Marshal(app.GetOAuthAuthorizationStatus(authorization.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-access-token", "secret-refresh-token"} {
		if strings.Contains(string(statusJSON), secret) {
			t.Fatalf("completed OAuth status leaked credential %q", secret)
		}
	}
}
