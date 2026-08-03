package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"antigravity-byok/internal/storage"
)

func TestAutomaticResponsesRoutingUsesChatForOrdinaryTurns(t *testing.T) {
	normalChat := map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "普通聊天"}}}},
		// The IDE commonly supplies function declarations on normal turns.
		// Chat Completions supports these, so they alone must not promote every
		// turn to Responses or attach hosted web/image tools.
		"tools": []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "run_terminal"}}}},
	}
	if requiresOpenAIResponses(normalChat) {
		t.Fatal("ordinary chat with function declarations unexpectedly selected Responses")
	}

	for name, request := range map[string]map[string]any{
		"attachment": {
			"contents": []any{map[string]any{"role": "user", "parts": []any{inlinePart("image/png", "aGVsbG8=")}}},
		},
		"native-web-tool": {
			"tools": []any{map[string]any{"type": "web_search"}},
		},
		"empty-native-web-tool": {
			"tools": []any{map[string]any{"googleSearch": map[string]any{}}},
		},
		"native-image-tool": {
			"tools": []any{map[string]any{"imageGeneration": map[string]any{"enabled": true}}},
		},
		"local-override": {
			"wfUseResponses": true,
		},
		"image-modality": {
			"generationConfig": map[string]any{"responseModalities": []any{"TEXT", "IMAGE"}},
		},
	} {
		if !requiresOpenAIResponses(request) {
			t.Fatalf("%s did not select Responses", name)
		}
	}
}

func TestForwardOpenAIAutoRoutesOrdinaryChatToChatCompletions(t *testing.T) {
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "ordinary chat must not call Responses", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chat-1\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer upstream.Close()

	model := &storage.CustomModel{Provider: "openai", APIURL: upstream.URL, APIKey: "test", ExternalModelName: "gpt-test", APIStyle: "auto"}
	recorder := httptest.NewRecorder()
	forwardOpenAI(recorder, httptest.NewRequest(http.MethodPost, "/generate", nil), model, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "普通聊天"}}}},
	}, "ordinary-chat")

	if len(paths) != 1 || paths[0] != "/v1/chat/completions" {
		t.Fatalf("upstream paths = %#v, want only chat completions", paths)
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"text":"ok"`) {
		t.Fatalf("unexpected downstream response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestResponsesBuiltinToolFallbackIsSafeAndRemembered(t *testing.T) {
	var bodies []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			http.Error(w, `{"error":{"message":"Unsupported tool type: web_search"}}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"model\":\"gpt-test\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"))
	}))
	defer upstream.Close()

	model := &storage.CustomModel{Provider: "openai", APIURL: upstream.URL, APIKey: "test", ExternalModelName: "gpt-test", APIStyle: "responses"}
	defer clearResponsesBuiltinToolCompatibility(model)
	gemini := map[string]any{
		"contents":        []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "回答"}}}},
		"webSearch":       true,
		"imageGeneration": true,
	}

	first := httptest.NewRecorder()
	forwardOpenAIResponses(first, httptest.NewRequest(http.MethodPost, "/generate", nil), model, gemini, "tools-fallback-one", false)
	if len(bodies) != 2 {
		t.Fatalf("upstream requests = %d, want 2 (explicit rejection + safe downgrade)", len(bodies))
	}
	if !requestContainsResponseTool(bodies[0], responseWebSearchTool) || requestContainsResponseTool(bodies[1], responseWebSearchTool) {
		t.Fatalf("web-search fallback did not remove only the rejected tool: %#v", bodies)
	}
	if !requestContainsResponseTool(bodies[1], responseImageGenerationTool) {
		t.Fatalf("unrejected image-generation tool was unexpectedly removed: %#v", bodies[1])
	}
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"text":"ok"`) {
		t.Fatalf("unexpected first downstream response: %d %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	forwardOpenAIResponses(second, httptest.NewRequest(http.MethodPost, "/generate", nil), model, gemini, "tools-fallback-two", false)
	if len(bodies) != 3 {
		t.Fatalf("known unsupported hosted tool was retried: requests = %d, want 3", len(bodies))
	}
	if requestContainsResponseTool(bodies[2], responseWebSearchTool) {
		t.Fatalf("remembered fallback still sent web search: %#v", bodies[2])
	}
	if second.Code != http.StatusOK {
		t.Fatalf("unexpected second downstream status: %d %s", second.Code, second.Body.String())
	}
}

func TestResponsesBuiltinFallbackDoesNotMaskFunctionSchemaError(t *testing.T) {
	request := map[string]any{"tools": []map[string]any{
		{"type": "function", "name": "command_status"},
		{"type": responseWebSearchTool},
	}}
	if rejected := rejectedResponsesBuiltinTools(http.StatusBadRequest, `{"error":{"message":"Invalid schema for function 'command_status': BOOLEAN is invalid"}}`, request); len(rejected) != 0 {
		t.Fatalf("function schema error incorrectly retried as a hosted-tool fallback: %#v", rejected)
	}
}

func TestResponsesStreamDoesNotReplayAfterAcceptedPartialOutput(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp-partial\",\"delta\":\"第一段\"}\n\n"))
		// Deliberately end before response.completed.
	}))
	defer upstream.Close()

	model := &storage.CustomModel{Provider: "openai", APIURL: upstream.URL, APIKey: "test", ExternalModelName: "gpt-test", APIStyle: "responses"}
	recorder := httptest.NewRecorder()
	forwardOpenAIResponses(recorder, httptest.NewRequest(http.MethodPost, "/generate", nil), model, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "请回答"}}}},
	}, "responses-partial", false)

	if calls != 1 {
		t.Fatalf("accepted partial Responses stream was replayed %d times", calls)
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "第一段") || !strings.Contains(recorder.Body.String(), `"finishReason":"STOP"`) {
		t.Fatalf("partial response was not safely closed: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestResponsesDoneMarkerWritesTerminalEvent(t *testing.T) {
	recorder := httptest.NewRecorder()
	outcome := streamOpenAIResponsesAttempt(newDownstreamSSEWriter(recorder), &http.Response{
		Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
	}, "responses-done", 1, nil)
	if !outcome.finished {
		t.Fatal("[DONE] did not finish the Responses stream")
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"finishReason":"STOP"`) {
		t.Fatalf("[DONE] did not produce an Antigravity terminal event: %d %s", recorder.Code, recorder.Body.String())
	}
}

func requestContainsResponseTool(request map[string]any, wanted string) bool {
	tools, _ := request["tools"].([]any)
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		if kind, _ := tool["type"].(string); kind == wanted {
			return true
		}
	}
	return false
}
