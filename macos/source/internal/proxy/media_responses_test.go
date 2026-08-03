package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"antigravity-byok/internal/storage"
)

func inlinePart(mimeType, data string) map[string]any {
	return map[string]any{"inlineData": map[string]any{"mimeType": mimeType, "data": data}}
}

func TestOpenAIChatKeepsImageAttachment(t *testing.T) {
	gemini := map[string]any{"contents": []any{map[string]any{
		"role": "user", "parts": []any{map[string]any{"text": "看图"}, inlinePart("image/png", "aGVsbG8=")},
	}}}
	request, err := toOpenAIRequestWithMedia(gemini, "gpt-test")
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	messages := request["messages"].([]map[string]any)
	content, ok := messages[0]["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("expected text and image content blocks, got %#v", messages[0]["content"])
	}
	image := content[1].(map[string]any)
	if image["type"] != "image_url" || image["image_url"].(map[string]any)["url"] != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("image attachment was not preserved: %#v", image)
	}
}

func TestAnthropicKeepsPDF(t *testing.T) {
	gemini := map[string]any{"contents": []any{map[string]any{
		"role": "user", "parts": []any{inlinePart("application/pdf", "cGRm")},
	}}}
	request, err := toAnthropicRequestWithMedia(gemini, "claude-test")
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	messages := request["messages"].([]map[string]any)
	blocks := messages[0]["content"].([]any)
	document := blocks[0].(map[string]any)
	if document["type"] != "document" || document["source"].(map[string]any)["data"] != "cGRm" {
		t.Fatalf("PDF attachment was not preserved: %#v", document)
	}
}

func TestResponsesKeepsGeneralFileAndTools(t *testing.T) {
	gemini := map[string]any{
		"contents": []any{map[string]any{
			"role": "user", "parts": []any{map[string]any{"text": "分析文件"}, inlinePart("application/pdf", "cGRm")},
		}},
		"tools": []any{map[string]any{"functionDeclarations": []any{map[string]any{
			"name": "read_file", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}},
		}}}},
	}
	model := &storage.CustomModel{Capabilities: storage.ModelCapabilities{Configured: true, SupportsWebSearch: true, SupportsImageGeneration: true}}
	request, err := toOpenAIResponsesRequest(gemini, "gpt-test", model)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	input := request["input"].([]any)
	blocks := input[0].(map[string]any)["content"].([]any)
	file := blocks[1].(map[string]any)
	if file["type"] != "input_file" || file["file_data"] != "data:application/pdf;base64,cGRm" {
		t.Fatalf("file attachment was not preserved: %#v", file)
	}
	tools := request["tools"].([]map[string]any)
	if len(tools) != 3 || tools[1]["type"] != "web_search" || tools[2]["type"] != "image_generation" {
		t.Fatalf("expected function, web search and image tools: %#v", tools)
	}
}

func TestResponsesStreamTextAndImage(t *testing.T) {
	state := &openAIResponsesStreamState{traceID: "test-trace"}
	textEvent := `data: {"type":"response.output_text.delta","response_id":"resp_1","delta":"你好"}`
	converted := convertOpenAIResponsesLineToGemini(textEvent, state)
	if !strings.Contains(converted, "你好") || !strings.Contains(converted, "test-trace") {
		t.Fatalf("text event conversion failed: %s", converted)
	}
	imageEvent := `data: {"type":"response.output_item.done","item":{"type":"image_generation_call","result":"aW1hZ2U="}}`
	converted = convertOpenAIResponsesLineToGemini(imageEvent, state)
	if !strings.Contains(converted, `"inlineData"`) || !strings.Contains(converted, "aW1hZ2U=") {
		t.Fatalf("image event conversion failed: %s", converted)
	}
	completed := convertOpenAIResponsesLineToGemini(`data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-test","usage":{"input_tokens":3,"output_tokens":2}}}`, state)
	var envelope map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(completed, "data: "), "\n\n")), &envelope); err != nil {
		t.Fatalf("invalid completion envelope: %v", err)
	}
	finish := envelope["response"].(map[string]any)["candidates"].([]any)[0].(map[string]any)["finishReason"]
	if finish != "STOP" {
		t.Fatalf("expected STOP, got %#v", finish)
	}
}
