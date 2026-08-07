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

func TestLoadEnabledModelsKeepsLegacyModelsAndFiltersExplicitlyDisabledModels(t *testing.T) {
	Init(t.TempDir())
	disabled := false
	if err := SaveModels([]CustomModel{
		{Name: "models/legacy", ExternalModelName: "legacy"},
		{Name: "models/disabled", ExternalModelName: "disabled", Enabled: &disabled},
	}); err != nil {
		t.Fatal(err)
	}

	models, err := LoadEnabledModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Name != "models/legacy" {
		t.Fatalf("enabled models = %#v, want only legacy model", models)
	}
}

func TestMergeDiscoveredAccountModelsOnlyPoolsEquivalentUpstreams(t *testing.T) {
	Init(t.TempDir())
	first := NewDiscoveredModel("openai", "https://api.example.test", "direct-key", "gpt-pool")
	first.DisplayName = "Keep this model label"
	first.AccountIDs = []string{"first"}
	first.APIStyle = "responses"
	first.Capabilities = ModelCapabilities{Configured: true, SupportsImages: true}

	secondEndpoint := NewDiscoveredModel("openai", "https://other.example.test", "", "gpt-pool")
	secondEndpoint.AccountIDs = []string{"other-endpoint"}
	secondProvider := NewDiscoveredModel("anthropic", "https://api.example.test", "", "gpt-pool")
	secondProvider.AccountIDs = []string{"other-provider"}

	initial, err := MergeDiscoveredAccountModels([]CustomModel{first, secondEndpoint, secondProvider})
	if err != nil || initial.Added != 3 || initial.Bound != 0 {
		t.Fatalf("initial merge = %+v, %v", initial, err)
	}

	sameUpstream := NewDiscoveredModel("openai", "https://api.example.test/", "", "gpt-pool")
	sameUpstream.AccountIDs = []string{"second", "first"}
	sameUpstream.APIStyle = "responses"
	merged, err := MergeDiscoveredAccountModels([]CustomModel{sameUpstream})
	if err != nil || merged.Added != 0 || merged.Bound != 1 || merged.Unchanged != 0 {
		t.Fatalf("matching upstream merge = %+v, %v", merged, err)
	}

	models, err := LoadModels()
	if err != nil || len(models) != 3 {
		t.Fatalf("models = %#v, %v", models, err)
	}
	for _, model := range models {
		if model.Provider == "openai" && strings.TrimRight(model.APIURL, "/") == "https://api.example.test" {
			if model.DisplayName != "Keep this model label" || model.APIStyle != "responses" {
				t.Fatalf("existing model configuration was overwritten: %#v", model)
			}
			if model.APIKey != "direct-key" {
				t.Fatalf("existing direct credential was overwritten: %#v", model)
			}
			if len(model.AccountIDs) != 2 || model.AccountIDs[0] != "first" || model.AccountIDs[1] != "second" {
				t.Fatalf("matching model pool = %#v", model.AccountIDs)
			}
		}
	}
}

func TestMergeDiscoveredAccountModelsSeparatesIncompatibleRouteContracts(t *testing.T) {
	Init(t.TempDir())
	base := NewDiscoveredModel("openai", "https://api.example.test", "", "gpt-route-pool")
	base.APIStyle = "responses"
	base.AccountIDs = []string{"responses-primary"}
	if result, err := MergeDiscoveredAccountModels([]CustomModel{base}); err != nil || result.Added != 1 {
		t.Fatalf("save base route = %+v, %v", result, err)
	}

	// Each candidate shares the same provider, endpoint, and upstream model ID,
	// but changes a field that can alter the request route. None may be added to
	// the base account pool.
	incompatible := []struct {
		name    string
		account string
		adjust  func(*CustomModel)
	}{
		{
			name: "API style", account: "chat-peer",
			adjust: func(model *CustomModel) { model.APIStyle = "chat_completions" },
		},
		{
			name: "message path", account: "compat-peer",
			adjust: func(model *CustomModel) { model.MessagePathMode = "compat" },
		},
		{
			name: "endpoint mode", account: "manual-peer",
			adjust: func(model *CustomModel) { model.EndpointMode = "manual" },
		},
	}
	for _, test := range incompatible {
		candidate := NewDiscoveredModel("openai", "https://api.example.test", "", "gpt-route-pool")
		candidate.APIStyle = "responses"
		candidate.AccountIDs = []string{test.account}
		test.adjust(&candidate)
		result, err := MergeDiscoveredAccountModels([]CustomModel{candidate})
		if err != nil || result.Added != 1 || result.Bound != 0 {
			t.Fatalf("%s route merge = %+v, %v", test.name, result, err)
		}
	}

	matching := NewDiscoveredModel("openai", "https://api.example.test/", "", "gpt-route-pool")
	matching.APIStyle = "responses"
	matching.AccountIDs = []string{"responses-peer"}
	result, err := MergeDiscoveredAccountModels([]CustomModel{matching})
	if err != nil || result.Added != 0 || result.Bound != 1 {
		t.Fatalf("matching route merge = %+v, %v", result, err)
	}

	models, err := LoadModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 4 {
		t.Fatalf("model count = %d, want 4 distinct route contracts", len(models))
	}
	seenNames := make(map[string]struct{}, len(models))
	var pooled *CustomModel
	for index := range models {
		model := &models[index]
		if _, exists := seenNames[model.Name]; exists {
			t.Fatalf("route-separated models reused internal name %q", model.Name)
		}
		seenNames[model.Name] = struct{}{}
		for _, accountID := range model.AccountIDs {
			if accountID == "responses-primary" {
				pooled = model
			}
		}
	}
	if pooled == nil {
		t.Fatalf("base route model not found in %#v", models)
	}
	if pooled.APIStyle != "responses" || pooled.EndpointMode != "auto" || pooled.MessagePathMode != "auto" {
		t.Fatalf("base route contract was changed: %#v", pooled)
	}
	if len(pooled.AccountIDs) != 2 || pooled.AccountIDs[0] != "responses-primary" || pooled.AccountIDs[1] != "responses-peer" {
		t.Fatalf("matching account was not the only pool addition: %#v", pooled.AccountIDs)
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
