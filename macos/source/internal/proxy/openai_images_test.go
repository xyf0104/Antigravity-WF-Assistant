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

func TestOpenAIImageDataFromResponseSupportsOpenAIBase64Shape(t *testing.T) {
	data, mimeType, err := openAIImageDataFromResponse([]byte(`{"data":[{"b64_json":"aGVsbG8=","mime_type":"image/webp"}]}`))
	if err != nil || data != "aGVsbG8=" || mimeType != "image/webp" {
		t.Fatalf("parsed image = %q, %q, %v", data, mimeType, err)
	}

	if _, _, err := openAIImageDataFromResponse([]byte(`{"data":[{"url":"https://example.test/generated.png"}]}`)); err == nil || !strings.Contains(err.Error(), "Base64") {
		t.Fatalf("URL-only response should require requested Base64 output, err = %v", err)
	}
}
