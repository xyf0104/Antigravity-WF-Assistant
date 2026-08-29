package codexconfig

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Inspect reads config.toml without mutating it. Provider secrets and header
// values are intentionally redacted; only their presence and header names are
// reported to the UI.
func (m *Manager) Inspect() (ConfigSnapshot, error) {
	if err := m.validatePaths(); err != nil {
		return ConfigSnapshot{}, err
	}
	data, exists, mode, err := readRegularFile(m.ConfigPath)
	location := ConfigLocation{CodexHome: m.CodexHome, ConfigPath: m.ConfigPath, Exists: exists}
	if err != nil {
		return ConfigSnapshot{Location: location}, err
	}
	if !exists {
		return ConfigSnapshot{Location: location, Valid: true, Context: DefaultContextSettings()}, nil
	}
	var root map[string]any
	if err := toml.Unmarshal(data, &root); err != nil {
		return ConfigSnapshot{Location: location, SHA256: sha256Hex(data), Mode: mode, Valid: false}, fmt.Errorf("parse config.toml: %w", err)
	}
	context, err := readContextSettingsFromRoot(root)
	if err != nil {
		return ConfigSnapshot{Location: location, SHA256: sha256Hex(data), Mode: mode, Valid: false}, fmt.Errorf("read config context settings: %w", err)
	}
	snapshot := ConfigSnapshot{
		Location:      location,
		SHA256:        sha256Hex(data),
		Mode:          mode,
		Valid:         true,
		ModelProvider: stringValue(root["model_provider"]),
		Model:         stringValue(root["model"]),
		ReviewModel:   stringValue(root["review_model"]),
		WebSearch:     webSearchValue(root["web_search"]),
		Context:       context,
		Providers:     providersFromRoot(root),
	}
	snapshot.ConfiguredModels = configuredModels(snapshot.Model, snapshot.ReviewModel)
	return snapshot, nil
}

// webSearchValue is a redacted, user-configurable top-level setting. Keep its
// observable mode in the snapshot so reopening the Codex modal and saving an
// unrelated field cannot silently turn cached or disabled search back to live.
func webSearchValue(value any) string {
	mode := strings.ToLower(strings.TrimSpace(stringValue(value)))
	switch mode {
	case "live", "cached", "disabled":
		return mode
	case "off":
		// Codex accepts this legacy spelling. Present the canonical UI value
		// without changing its disabled semantics on the next explicit save.
		return "disabled"
	default:
		return ""
	}
}

// ReadContextSettings reads only the two first-party-managed context values.
// Missing config values use Codex-compatible defaults.
func (m *Manager) ReadContextSettings() (ContextSettings, error) {
	if err := m.validatePaths(); err != nil {
		return DefaultContextSettings(), err
	}
	data, exists, _, err := readRegularFile(m.ConfigPath)
	if err != nil {
		return DefaultContextSettings(), err
	}
	if !exists || len(strings.TrimSpace(string(data))) == 0 {
		return DefaultContextSettings(), nil
	}
	var root map[string]any
	if err := toml.Unmarshal(data, &root); err != nil {
		return DefaultContextSettings(), fmt.Errorf("read config.toml: %w", err)
	}
	return readContextSettingsFromRoot(root)
}

// Verify validates the on-disk TOML and returns its redacted snapshot. It is a
// useful guard after a UI operation or before presenting an existing config.
func (m *Manager) Verify() (ConfigSnapshot, error) {
	snapshot, err := m.Inspect()
	if err != nil {
		return snapshot, err
	}
	if !snapshot.Valid {
		return snapshot, errors.New("config.toml is invalid")
	}
	return snapshot, nil
}

func providersFromRoot(root map[string]any) []Provider {
	providersRoot, ok := mapValue(root["model_providers"])
	if !ok {
		return nil
	}
	providers := make([]Provider, 0, len(providersRoot))
	for _, id := range sortedStringKeys(providersRoot) {
		raw, ok := mapValue(providersRoot[id])
		if !ok {
			continue
		}
		provider := Provider{
			ID:                    id,
			Name:                  stringValue(raw["name"]),
			BaseURL:               stringValue(raw["base_url"]),
			WireAPI:               stringValue(raw["wire_api"]),
			RequiresOpenAIAuth:    boolValue(raw["requires_openai_auth"]),
			SupportsWebSockets:    boolValue(raw["supports_websockets"]),
			HasExperimentalBearer: strings.TrimSpace(stringValue(raw["experimental_bearer_token"])) != "",
		}
		if headers, ok := mapValue(raw["http_headers"]); ok {
			provider.HeaderNames = sortedStringKeys(headers)
		}
		providers = append(providers, provider)
	}
	return providers
}

func configuredModels(model, review string) []string {
	set := map[string]struct{}{}
	for _, candidate := range []string{strings.TrimSpace(model), strings.TrimSpace(review)} {
		if candidate != "" {
			set[candidate] = struct{}{}
		}
	}
	models := make([]string, 0, len(set))
	for model := range set {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

func boolValue(value any) bool {
	parsed, _ := value.(bool)
	return parsed
}
