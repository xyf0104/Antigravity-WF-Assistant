package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestDiscoverModelsUsesConfiguredAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-a"},{"id":"claude-b"}]}`))
	}))
	defer server.Close()

	result := DiscoverModels(context.Background(), Config{Provider: "openai", APIURL: server.URL + "/v1", APIKey: "token", AuthMode: "bearer"})
	if !result.OK || len(result.Models) != 2 || result.Models[0].ID != "claude-b" {
		t.Fatalf("unexpected discovery result: %#v", result)
	}
}

func TestAutoModelTestFallsBackOnlyForMissingResponsesEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			http.NotFound(w, r)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chatcmpl-test"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result := TestModel(context.Background(), Config{Provider: "openai", APIURL: server.URL + "/v1", APIKey: "token", APIStyle: "auto"}, "gpt-test")
	if !result.OK || result.APIStyle != "chat_completions" {
		t.Fatalf("expected Responses fallback to chat, got %#v", result)
	}
}

func TestCustomHeaderAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://example.com/v1/models", nil)
	err := ApplyCredentials(request, Config{
		Provider: "custom", APIURL: "https://example.com/v1", APIKey: "secret", AuthMode: "custom_header", AuthHeader: "X-Token",
		Headers: map[string]string{"X-Client": "WF"},
	})
	if err != nil {
		t.Fatalf("ApplyCredentials failed: %v", err)
	}
	if request.Header.Get("X-Token") != "secret" || request.Header.Get("X-Client") != "WF" {
		t.Fatalf("headers not applied: %#v", request.Header)
	}
}

func TestManualEndpointIsPreservedExactly(t *testing.T) {
	config := Config{
		Provider:     "openai",
		APIURL:       "https://gateway.example.com/custom/chat?workspace=wf",
		EndpointMode: "manual",
	}

	for name, resolve := range map[string]func(Config) (string, error){
		"chat":      ResolveChatCompletionsURLForConfig,
		"responses": ResolveResponsesURLForConfig,
	} {
		got, err := resolve(config)
		if err != nil {
			t.Fatalf("%s resolver returned error: %v", name, err)
		}
		if want := config.APIURL; got != want {
			t.Errorf("%s manual endpoint = %q, want %q", name, got, want)
		}
	}
}

func TestAnthropicEndpointModes(t *testing.T) {
	base := Config{Provider: "anthropic", APIURL: "https://api.example.com"}

	auto, err := ResolveAnthropicMessageCandidates(base)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://api.example.com/v1/messages", "https://api.example.com/v1/chat/messages"}; !reflect.DeepEqual(auto, want) {
		t.Fatalf("automatic Claude candidates = %#v, want %#v", auto, want)
	}

	compat, err := ResolveAnthropicMessageCandidates(Config{Provider: "anthropic", APIURL: base.APIURL, MessagePathMode: "compat"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://api.example.com/v1/chat/messages"}; !reflect.DeepEqual(compat, want) {
		t.Fatalf("compatible Claude candidates = %#v, want %#v", compat, want)
	}

	manualURL := "https://api.example.com/claude/messages-v2?tenant=wf"
	manual, err := ResolveAnthropicMessageCandidates(Config{Provider: "anthropic", APIURL: manualURL, EndpointMode: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{manualURL}; !reflect.DeepEqual(manual, want) {
		t.Fatalf("manual Claude candidates = %#v, want %#v", manual, want)
	}
}
