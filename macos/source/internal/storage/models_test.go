package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestModelStoreUsesPrivateAtomicFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	Init(dir)
	model := CustomModel{
		Name: "models/test", DisplayName: "Test", Provider: "openai",
		APIKey: "secret", APIURL: "https://example.com/v1", ExternalModelName: "gpt-test",
	}
	if err := SaveModels([]CustomModel{model}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "custom_models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("model file permissions = %o, want 600", info.Mode().Perm())
	}
	loaded, err := LoadModels()
	if err != nil || len(loaded) != 1 || loaded[0].APIKey != model.APIKey {
		t.Fatalf("round trip failed: %+v, %v", loaded, err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".custom-models-*"))
	if len(matches) != 0 {
		t.Fatalf("temporary files were left behind: %v", matches)
	}
}

func TestEmptyDisplayNameUsesUpstreamModelName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	Init(dir)
	model := CustomModel{
		Name: "models/gpt-test", Provider: "openai", APIKey: "secret",
		APIURL: "https://example.com/v1", ExternalModelName: "gpt-test",
	}
	if err := AddOrUpdateModel(model); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadModels()
	if err != nil || len(loaded) != 1 {
		t.Fatalf("load failed: %+v, %v", loaded, err)
	}
	if loaded[0].DisplayName != "gpt-test" {
		t.Fatalf("displayName = %q, want upstream model name", loaded[0].DisplayName)
	}
}

func TestDefaultCapabilitiesExposeCompleteChatSurface(t *testing.T) {
	capabilities := DefaultCapabilities("openai", "gpt-5")
	if !capabilities.SupportsImages || !capabilities.SupportsFiles || capabilities.SupportsAudio || capabilities.SupportsVideo ||
		!capabilities.SupportsToolCalls || !capabilities.SupportsThinking || !capabilities.SupportsWebSearch || !capabilities.SupportsImageGeneration {
		t.Fatalf("normal chat model must receive the full default surface: %+v", capabilities)
	}
	if len(capabilities.SupportedMimeTypes) == 0 {
		t.Fatal("full capability profile must advertise attachment MIME types")
	}
}

func TestDefaultCapabilitiesKeepNonChatModelsConservative(t *testing.T) {
	capabilities := DefaultCapabilities("openai", "text-embedding-3-large")
	if capabilities.SupportsImages || capabilities.SupportsFiles || capabilities.SupportsToolCalls || capabilities.SupportsWebSearch || capabilities.SupportsImageGeneration {
		t.Fatalf("non-chat model must not advertise chat capabilities: %+v", capabilities)
	}
}

func TestEffectiveCapabilitiesDoNotReExposeUnsupportedLegacyMediaOrTools(t *testing.T) {
	legacy := CustomModel{
		Provider: "anthropic", APIStyle: "messages", ExternalModelName: "claude-test",
		Capabilities: ModelCapabilities{
			Configured: true, SupportsImages: true, SupportsFiles: true, SupportsAudio: true, SupportsVideo: true,
			SupportsWebSearch: true, SupportsImageGeneration: true,
			SupportedMimeTypes: []string{"image/png", "application/pdf", "audio/mpeg", "video/mp4"},
		},
	}
	capabilities := EffectiveCapabilities(legacy)
	if capabilities.SupportsAudio || capabilities.SupportsVideo || capabilities.SupportsWebSearch || capabilities.SupportsImageGeneration {
		t.Fatalf("Claude Messages must not advertise unsupported media or Responses tools: %+v", capabilities)
	}
	for _, mimeType := range capabilities.SupportedMimeTypes {
		if strings.HasPrefix(mimeType, "audio/") || strings.HasPrefix(mimeType, "video/") {
			t.Fatalf("legacy unsupported MIME type survived migration: %q", mimeType)
		}
	}
}

func TestEffectiveCapabilitiesKeepResponsesToolsForOpenAIAuto(t *testing.T) {
	capabilities := EffectiveCapabilities(CustomModel{
		Provider: "openai", APIStyle: "auto", ExternalModelName: "gpt-test",
		Capabilities: ModelCapabilities{Configured: true, SupportsWebSearch: true, SupportsImageGeneration: true},
	})
	if !capabilities.SupportsWebSearch || !capabilities.SupportsImageGeneration {
		t.Fatalf("OpenAI automatic routing must retain Responses tools: %+v", capabilities)
	}

	chatOnly := EffectiveCapabilities(CustomModel{
		Provider: "openai", APIStyle: "chat_completions", ExternalModelName: "gpt-test",
		Capabilities: ModelCapabilities{Configured: true, SupportsWebSearch: true, SupportsImageGeneration: true},
	})
	if chatOnly.SupportsWebSearch || chatOnly.SupportsImageGeneration {
		t.Fatalf("Chat-only endpoint must not advertise Responses tools: %+v", chatOnly)
	}
}
