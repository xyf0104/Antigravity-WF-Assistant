package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"antigravity-byok/internal/storage"
)

func setupAntigravityIntegrationModel(t *testing.T, model storage.CustomModel) {
	t.Helper()
	dir := t.TempDir()
	storage.Init(dir)
	InitTrace(dir)
	if err := storage.SaveModels([]storage.CustomModel{model}); err != nil {
		t.Fatalf("save custom model: %v", err)
	}
}

func antigravityRequest(modelID, requestID string, request map[string]any) *http.Request {
	payload, _ := json.Marshal(map[string]any{
		"model": modelID, "requestId": requestID, "request": request,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1internal:streamGenerateContent", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func textTurn(text string) map[string]any {
	return map[string]any{"contents": []any{map[string]any{
		"role": "user", "parts": []any{map[string]any{"text": text}},
	}}}
}

func writeOpenAIChatStream(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"id":"chat-integration","model":"gpt-integration","choices":[{"delta":{"content":"`+text+`"},"finish_reason":null}]}`+"\n\n")
	_, _ = io.WriteString(w, `data: {"id":"chat-integration","model":"gpt-integration","choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func TestAntigravityOpenAITextUsesChatCompletions(t *testing.T) {
	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("OpenAI text path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing OpenAI authorization: %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		writeOpenAIChatStream(w, "text-ok")
	}))
	defer upstream.Close()

	model := storage.CustomModel{Name: "models/integration-gpt", Provider: "openai", APIURL: upstream.URL, APIKey: "test-key", ExternalModelName: "gpt-integration", APIStyle: "auto"}
	setupAntigravityIntegrationModel(t, model)
	recorder := httptest.NewRecorder()
	handleRequest(recorder, antigravityRequest(model.Name, "text-request", textTurn("hello")))

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "text-ok") {
		t.Fatalf("downstream text response = %d %s", recorder.Code, recorder.Body.String())
	}
	messages, _ := received["messages"].([]any)
	if len(messages) != 1 || messages[0].(map[string]any)["content"] != "hello" {
		t.Fatalf("Antigravity text was not converted once into Chat Completions: %#v", received)
	}
}

func TestAntigravityOpenAIImageInputUsesResponses(t *testing.T) {
	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("OpenAI image input path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"id":"resp-image-input","model":"gpt-integration","output":[{"type":"message","content":[{"type":"output_text","text":"vision-ok"}]}]}}`+"\n\n")
	}))
	defer upstream.Close()

	model := storage.CustomModel{Name: "models/integration-image", Provider: "openai", APIURL: upstream.URL, APIKey: "test-key", ExternalModelName: "gpt-integration", APIStyle: "auto"}
	setupAntigravityIntegrationModel(t, model)
	request := textTurn("describe image")
	request["contents"].([]any)[0].(map[string]any)["parts"] = []any{
		map[string]any{"text": "describe image"},
		map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "aGVsbG8="}},
	}
	recorder := httptest.NewRecorder()
	handleRequest(recorder, antigravityRequest(model.Name, "image-input-request", request))

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "vision-ok") {
		t.Fatalf("downstream image response = %d %s", recorder.Code, recorder.Body.String())
	}
	input, _ := received["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("Responses input = %#v", received)
	}
	blocks, _ := input[0].(map[string]any)["content"].([]any)
	if len(blocks) != 2 || blocks[1].(map[string]any)["type"] != "input_image" || !strings.Contains(blocks[1].(map[string]any)["image_url"].(string), "aGVsbG8=") {
		t.Fatalf("image was not preserved as a Responses input_image: %#v", input)
	}
}

func TestAntigravityOpenAIImageGenerationUsesResponsesAndReturnsImage(t *testing.T) {
	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("OpenAI image generation path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.output_item.done","item":{"type":"image_generation_call","result":"aW1hZ2U="}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"id":"resp-image-output","model":"gpt-integration"}}`+"\n\n")
	}))
	defer upstream.Close()

	model := storage.CustomModel{Name: "models/integration-image-gen", Provider: "openai", APIURL: upstream.URL, APIKey: "test-key", ExternalModelName: "gpt-integration", APIStyle: "auto"}
	setupAntigravityIntegrationModel(t, model)
	request := textTurn("generate an image")
	request["generationConfig"] = map[string]any{"responseModalities": []any{"TEXT", "IMAGE"}}
	recorder := httptest.NewRecorder()
	handleRequest(recorder, antigravityRequest(model.Name, "image-generation-request", request))

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "inlineData") || !strings.Contains(recorder.Body.String(), "aW1hZ2U=") {
		t.Fatalf("image generation result was not converted for Antigravity: %d %s", recorder.Code, recorder.Body.String())
	}
	tools, _ := received["tools"].([]any)
	foundImageGeneration := false
	for _, raw := range tools {
		if raw.(map[string]any)["type"] == responseImageGenerationTool {
			foundImageGeneration = true
		}
	}
	if !foundImageGeneration {
		t.Fatalf("Responses request did not include image_generation: %#v", received)
	}
}

func TestAntigravityClaudeTextAndImageUseMessages(t *testing.T) {
	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("Claude path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "claude-key" || r.Header.Get("anthropic-version") == "" {
			t.Fatalf("Claude credentials were not applied: %#v", r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg-integration","model":"claude-integration"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"claude-ok"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	defer upstream.Close()

	model := storage.CustomModel{Name: "models/integration-claude", Provider: "anthropic", APIURL: upstream.URL, APIKey: "claude-key", ExternalModelName: "claude-integration", APIStyle: "messages", AuthMode: "x_api_key"}
	setupAntigravityIntegrationModel(t, model)
	request := textTurn("describe image")
	request["contents"].([]any)[0].(map[string]any)["parts"] = []any{
		map[string]any{"text": "describe image"},
		map[string]any{"inlineData": map[string]any{"mimeType": "image/jpeg", "data": "aGVsbG8="}},
	}
	recorder := httptest.NewRecorder()
	handleRequest(recorder, antigravityRequest(model.Name, "claude-image-request", request))

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "claude-ok") {
		t.Fatalf("downstream Claude response = %d %s", recorder.Code, recorder.Body.String())
	}
	messages, _ := received["messages"].([]any)
	blocks, _ := messages[0].(map[string]any)["content"].([]any)
	if len(blocks) != 2 || blocks[1].(map[string]any)["type"] != "image" {
		t.Fatalf("image was not preserved in Claude Messages payload: %#v", messages)
	}
}

func TestAntigravityOverlappingDuplicateDoesNotCreateSecondUpstreamGeneration(t *testing.T) {
	var upstreamCalls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if upstreamCalls.Add(1) == 1 {
			close(started)
		}
		<-release
		writeOpenAIChatStream(w, "once")
	}))
	defer upstream.Close()

	model := storage.CustomModel{Name: "models/integration-dedupe", Provider: "openai", APIURL: upstream.URL, APIKey: "test-key", ExternalModelName: "gpt-integration", APIStyle: "chat_completions"}
	setupAntigravityIntegrationModel(t, model)
	request := textTurn("only one generation")
	firstRecorder := httptest.NewRecorder()
	firstDone := make(chan struct{})
	go func() {
		handleRequest(firstRecorder, antigravityRequest(model.Name, "attempt-one", request))
		close(firstDone)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first upstream generation did not start")
	}

	secondRecorder := httptest.NewRecorder()
	handleRequest(secondRecorder, antigravityRequest(model.Name, "attempt-two", request))
	if secondRecorder.Code != http.StatusConflict {
		t.Fatalf("overlapping request was not suppressed: %d %s", secondRecorder.Code, secondRecorder.Body.String())
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("duplicate request reached upstream %d times", upstreamCalls.Load())
	}
	close(release)
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first upstream generation did not finish")
	}
	if strings.Count(firstRecorder.Body.String(), `"text":"once"`) != 1 {
		t.Fatalf("first generation emitted duplicate assistant text: %s", firstRecorder.Body.String())
	}
}
