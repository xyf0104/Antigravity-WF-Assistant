package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"antigravity-byok/internal/codexconfig"
	"antigravity-byok/internal/codexselection"
)

func TestCodexXIASSSelectionAppliesNativeOnlyKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	service := codexselection.New()
	t.Cleanup(service.Close)
	app := &App{codexKeySelection: service}
	started, err := service.Begin("https://gateway.example.test")
	if err != nil {
		t.Fatal(err)
	}
	callback, state := selectionCallbackAndState(t, started.ConnectURL)
	secret := "sk-native-selection-secret"
	completed := app.CompleteCodexXIASSKeySelectionManual(started.State.SessionID, selectionCallbackURL(callback, state, "https://gateway.example.test/v1", secret, "Primary Key"))
	if !completed.OK || completed.Selection.Status != "ready" {
		t.Fatalf("manual selection completion = %+v", completed)
	}

	status := app.ApplyCodexXIASSSelection(started.State.SessionID, codexconfig.ApplyConfig{
		ProviderName: "Project gateway",
		Model:        "gpt-5.6-sol",
		ReviewModel:  "gpt-5.6-sol",
		WebSearch:    "cached",
	})
	if !status.OK {
		t.Fatalf("ApplyCodexXIASSSelection() = %+v", status)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("configuration status leaked selection API key: %s", encoded)
	}
	written, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{secret, `base_url = "https://gateway.example.test/v1"`, `name = "Project gateway"`, `web_search = "cached"`} {
		if !strings.Contains(string(written), want) {
			t.Fatalf("written Codex config does not contain %q:\n%s", want, written)
		}
	}
	if repeated := app.ApplyCodexXIASSSelection(started.State.SessionID, codexconfig.ApplyConfig{Model: "gpt-5.6-sol"}); repeated.OK {
		t.Fatalf("consumed selection unexpectedly applied again: %+v", repeated)
	}
}

func TestExitCleanupClosesCodexKeySelectionListener(t *testing.T) {
	service := codexselection.New()
	started, err := service.Begin("https://gateway.example.test")
	if err != nil {
		t.Fatal(err)
	}
	callback, _ := selectionCallbackAndState(t, started.ConnectURL)
	app := &App{codexKeySelection: service}
	app.releaseExitResources()
	connection, err := net.DialTimeout("tcp", callback.Host, 200*time.Millisecond)
	if err == nil {
		connection.Close()
		t.Fatalf("Codex selection listener %s still accepts connections after exit cleanup", callback.Host)
	}
}

func selectionCallbackAndState(t *testing.T, connectURL string) (*url.URL, string) {
	t.Helper()
	parsed, err := url.Parse(connectURL)
	if err != nil {
		t.Fatal(err)
	}
	callback, err := url.Parse(parsed.Query().Get("callback"))
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("missing selection callback state")
	}
	return callback, state
}

func selectionCallbackURL(callback *url.URL, state, baseURL, apiKey, keyName string) string {
	payload, err := json.Marshal(map[string]string{"base_url": baseURL, "api_key": apiKey, "key_name": keyName})
	if err != nil {
		panic(err)
	}
	result := *callback
	result.Fragment = url.Values{
		"state":   []string{state},
		"payload": []string{base64.RawURLEncoding.EncodeToString(payload)},
	}.Encode()
	return result.String()
}
