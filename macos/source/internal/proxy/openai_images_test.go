package proxy

import (
	"strings"
	"testing"

	"antigravity-wf-assistant/internal/storage"
)

func TestDirectOpenAIImageModelUsesOnlyEnabledModelsFromSameUpstream(t *testing.T) {
	storage.Init(t.TempDir())
	disabled := false
	enabled := true
	selected := storage.CustomModel{
		Name: "models/sol", Provider: "openai", APIURL: "https://api.example.test", APIKey: "sol-key", ExternalModelName: "gpt-5.6-sol",
	}
	if err := storage.SaveModels([]storage.CustomModel{
		selected,
		{Name: "models/disabled-image", Provider: "openai", APIURL: selected.APIURL, ExternalModelName: "gpt-image-2", Enabled: &disabled},
		{Name: "models/other-upstream-image", Provider: "openai", APIURL: "https://other.example.test", ExternalModelName: "gpt-image-2", Enabled: &enabled},
		{Name: "models/same-upstream-image", Provider: "openai", APIURL: selected.APIURL, ExternalModelName: "gpt-image-1", Enabled: &enabled},
	}); err != nil {
		t.Fatal(err)
	}

	image := directOpenAIImageModel(&selected)
	if image == nil || image.ExternalModelName != "gpt-image-1" {
		t.Fatalf("selected image model = %#v, want enabled same-upstream gpt-image-1", image)
	}
}

func TestDirectOpenAIImageModelFallsBackToSeparateEnabledSupplier(t *testing.T) {
	storage.Init(t.TempDir())
	enabled := true
	selected := storage.CustomModel{
		Name: "models/sol", Provider: "openai", APIURL: "https://chat.example.test", APIKey: "chat-key", ExternalModelName: "gpt-5.6-sol",
	}
	image := storage.CustomModel{
		Name: "models/image", Provider: "openai", APIURL: "https://images.example.test", APIKey: "image-key", ExternalModelName: "gpt-image-2", Enabled: &enabled,
	}
	if err := storage.SaveModels([]storage.CustomModel{selected, image}); err != nil {
		t.Fatal(err)
	}

	resolved := directOpenAIImageModel(&selected)
	if resolved == nil || resolved.Name != image.Name || resolved.APIURL != image.APIURL {
		t.Fatalf("cross-supplier image model = %#v, want %#v", resolved, image)
	}
	execution := directOpenAIImageExecutionModel(&selected, resolved)
	if execution == nil || execution.APIKey != "image-key" || execution.APIURL != image.APIURL {
		t.Fatalf("cross-supplier image execution did not retain its own credentials: %#v", execution)
	}
}

func TestOpenAIImageDataFromResponseSupportsOpenAIBase64Shape(t *testing.T) {
	data, mimeType, err := openAIImageDataFromResponse([]byte(`{"data":[{"b64_json":"aGVsbG8=","mime_type":"image/webp"}]}`))
	if err != nil || data != "aGVsbG8=" || mimeType != "image/webp" {
		t.Fatalf("parsed image = %q, %q, %v", data, mimeType, err)
	}

	if _, _, err := openAIImageDataFromResponse([]byte(`{"data":[{"url":"https://example.test/generated.png"}]}`)); err == nil || !strings.Contains(err.Error(), "Base64") {
		t.Fatalf("URL-only response should require requested Base64 output, err = %v", err)
	}
}

func TestSanitizeImageGenerationPromptExcludesAntigravityMCPRules(t *testing.T) {
	gemini := map[string]any{
		"systemInstruction": map[string]any{"parts": []any{map[string]any{
			"text": "<mcp>rules: call command_status and inspect the workspace</mcp>",
		}}},
		"contents": []any{map[string]any{
			"role":  "user",
			"parts": []any{map[string]any{"text": "<system>MCP rules: never send this to an image model</system>\n用户请求：生成一张橙色宇航猫图片"}},
		}},
	}
	prompt := directImageGenerationPrompt(gemini)
	if prompt != "生成一张橙色宇航猫图片" {
		t.Fatalf("image prompt included internal instructions: %q", prompt)
	}
	if strings.Contains(strings.ToLower(prompt), "mcp") || strings.Contains(strings.ToLower(prompt), "command_status") {
		t.Fatalf("image prompt leaked Antigravity MCP rules: %q", prompt)
	}
}
