package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// ModelCapabilities declares the features which are safe to advertise to
// Antigravity for a custom upstream model. Configured is deliberately kept
// separate from the booleans: older configs did not contain capability data,
// while a user may explicitly disable an otherwise common capability.
type ModelCapabilities struct {
	Configured              bool     `json:"configured,omitempty"`
	SupportsImages          bool     `json:"supportsImages,omitempty"`
	SupportsFiles           bool     `json:"supportsFiles,omitempty"`
	SupportsAudio           bool     `json:"supportsAudio,omitempty"`
	SupportsVideo           bool     `json:"supportsVideo,omitempty"`
	SupportsToolCalls       bool     `json:"supportsToolCalls,omitempty"`
	SupportsWebSearch       bool     `json:"supportsWebSearch,omitempty"`
	SupportsImageGeneration bool     `json:"supportsImageGeneration,omitempty"`
	SupportsThinking        bool     `json:"supportsThinking,omitempty"`
	SupportedMimeTypes      []string `json:"supportedMimeTypes,omitempty"`
}

// CommonMediaMimeTypes are formats Antigravity can attach in its native chat
// UI. A model is only told about formats that its saved capability profile
// enables, so the picker does not allow an attachment that the proxy will
// subsequently discard.
var CommonMediaMimeTypes = []string{
	"image/png", "image/jpeg", "image/webp", "image/gif", "image/svg+xml",
	"text/plain", "text/markdown", "text/html", "text/css", "text/xml", "text/csv",
	"application/json", "application/pdf", "application/x-javascript",
	"application/x-typescript", "application/x-python-code", "application/x-ipynb+json",
}

var audioMimeTypes = []string{"audio/mpeg", "audio/mp4", "audio/wav", "audio/webm", "audio/ogg"}
var videoMimeTypes = []string{"video/mp4", "video/webm", "video/quicktime"}

// DefaultCapabilities is intentionally conservative for generation and web
// search: these require an upstream API surface that not every compatible
// endpoint implements. Image/file input, tools and thinking are enabled for
// mainstream chat models and remain editable in the assistant UI.
func DefaultCapabilities(provider, modelName string) ModelCapabilities {
	name := strings.ToLower(strings.TrimSpace(modelName))
	nonChat := strings.Contains(name, "embedding") || strings.Contains(name, "tts") || strings.Contains(name, "whisper")
	capabilities := ModelCapabilities{
		SupportsImages:    !nonChat,
		SupportsFiles:     !nonChat,
		SupportsToolCalls: !nonChat,
		SupportsThinking:  !nonChat,
	}
	if strings.Contains(name, "gpt-image") || strings.Contains(name, "image-1") {
		capabilities.SupportsImageGeneration = true
	}
	if strings.EqualFold(provider, "anthropic") {
		// Anthropic Messages supports images/documents and tool use. Web search is
		// intentionally opt-in because it depends on the account/model beta.
		capabilities.SupportsImages = !nonChat
		capabilities.SupportsFiles = !nonChat
	}
	capabilities.SupportedMimeTypes = capabilityMimeTypes(capabilities)
	return capabilities
}

func capabilityMimeTypes(capabilities ModelCapabilities) []string {
	if len(capabilities.SupportedMimeTypes) > 0 {
		return normalizeMimeTypes(capabilities.SupportedMimeTypes)
	}
	var result []string
	if capabilities.SupportsImages || capabilities.SupportsFiles {
		result = append(result, CommonMediaMimeTypes...)
	}
	if capabilities.SupportsAudio {
		result = append(result, audioMimeTypes...)
	}
	if capabilities.SupportsVideo {
		result = append(result, videoMimeTypes...)
	}
	return normalizeMimeTypes(result)
}

func normalizeMimeTypes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || !strings.Contains(value, "/") {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// EffectiveCapabilities migrates old JSON lazily without mutating it on read.
func EffectiveCapabilities(model CustomModel) ModelCapabilities {
	capabilities := model.Capabilities
	if !capabilities.Configured {
		return DefaultCapabilities(model.Provider, model.ExternalModelName)
	}
	capabilities.SupportedMimeTypes = capabilityMimeTypes(capabilities)
	return capabilities
}

// CustomModel represents a third-party model configuration.
type CustomModel struct {
	Name              string            `json:"name"`
	DisplayName       string            `json:"displayName"`
	Description       string            `json:"description"`
	Provider          string            `json:"provider"` // "openai" | "anthropic" | "grok" | "custom"
	APIKey            string            `json:"apiKey"`
	APIURL            string            `json:"apiUrl"`
	ExternalModelName string            `json:"externalModelName"`
	ReasoningEffort   string            `json:"reasoningEffort,omitempty"` // "auto" | "none" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max"
	APIStyle          string            `json:"apiStyle,omitempty"`        // "auto" | "chat_completions" | "responses" | "messages"
	AuthMode          string            `json:"authMode,omitempty"`        // "bearer" | "x_api_key" | "custom_header"
	AuthHeader        string            `json:"authHeader,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Capabilities      ModelCapabilities `json:"capabilities,omitempty"`
}

type modelsStore struct {
	Models []CustomModel `json:"models"`
}

var (
	mu         sync.RWMutex
	storageDir string
	modelsFile string
)

func Init(dir string) {
	storageDir = dir
	modelsFile = filepath.Join(dir, "custom_models.json")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o700)
	_ = os.Chmod(modelsFile, 0o600)
}

func StorageDir() string { return storageDir }

func LoadModels() ([]CustomModel, error) {
	mu.RLock()
	defer mu.RUnlock()
	return loadModelsLocked()
}

func loadModelsLocked() ([]CustomModel, error) {
	data, err := os.ReadFile(modelsFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var store modelsStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	for i := range store.Models {
		store.Models[i] = normalizeModelDisplayName(store.Models[i])
	}
	return store.Models, nil
}

func SaveModels(models []CustomModel) error {
	mu.Lock()
	defer mu.Unlock()
	for i := range models {
		models[i] = normalizeModelDisplayName(models[i])
	}
	return saveModelsLocked(models)
}

func AddOrUpdateModel(m CustomModel) error {
	mu.Lock()
	defer mu.Unlock()
	m = normalizeModelDisplayName(m)
	models, err := loadModelsLocked()
	if err != nil {
		return err
	}
	for i, existing := range models {
		if existing.Name == m.Name {
			models[i] = m
			return saveModelsLocked(models)
		}
	}
	models = append(models, m)
	return saveModelsLocked(models)
}

func normalizeModelDisplayName(model CustomModel) CustomModel {
	model.Provider = strings.ToLower(strings.TrimSpace(model.Provider))
	if model.Provider == "" {
		model.Provider = "openai"
	}
	model.APIStyle = strings.ToLower(strings.TrimSpace(model.APIStyle))
	model.AuthMode = strings.ToLower(strings.TrimSpace(model.AuthMode))
	model.AuthHeader = strings.TrimSpace(model.AuthHeader)
	model.ExternalModelName = strings.TrimSpace(model.ExternalModelName)
	model.Name = strings.TrimSpace(model.Name)
	if strings.TrimSpace(model.DisplayName) == "" {
		model.DisplayName = strings.TrimSpace(model.ExternalModelName)
	}
	model.DisplayName = strings.TrimSpace(model.DisplayName)
	model.Capabilities.SupportedMimeTypes = capabilityMimeTypes(model.Capabilities)
	return model
}

var modelNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// NewDiscoveredModel creates a collision-resistant internal name while keeping
// the user-facing name equal to the upstream model id by default.
func NewDiscoveredModel(provider, apiURL, apiKey, externalModelName string) CustomModel {
	provider = strings.ToLower(strings.TrimSpace(provider))
	externalModelName = strings.TrimSpace(strings.TrimPrefix(externalModelName, "models/"))
	namePart := strings.Trim(modelNameUnsafe.ReplaceAllString(strings.ToLower(externalModelName), "-"), "-")
	if namePart == "" {
		namePart = "model"
	}
	endpointPart := strings.Trim(modelNameUnsafe.ReplaceAllString(strings.ToLower(strings.TrimSpace(apiURL)), "-"), "-")
	if len(endpointPart) > 24 {
		endpointPart = endpointPart[len(endpointPart)-24:]
	}
	if endpointPart == "" {
		endpointPart = "upstream"
	}
	return normalizeModelDisplayName(CustomModel{
		Name:              fmt.Sprintf("models/%s-%s-%s", provider, namePart, endpointPart),
		DisplayName:       externalModelName,
		Provider:          provider,
		APIKey:            apiKey,
		APIURL:            apiURL,
		ExternalModelName: externalModelName,
		APIStyle:          "auto",
		AuthMode:          "bearer",
		Capabilities:      DefaultCapabilities(provider, externalModelName),
	})
}

func DeleteModel(name string) error {
	mu.Lock()
	defer mu.Unlock()
	models, err := loadModelsLocked()
	if err != nil {
		return err
	}
	out := models[:0]
	for _, m := range models {
		if m.Name != name {
			out = append(out, m)
		}
	}
	return saveModelsLocked(out)
}

func saveModelsLocked(models []CustomModel) error {
	data, err := json.MarshalIndent(modelsStore{Models: models}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(modelsFile), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(modelsFile), ".custom-models-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceStorageFile(tempPath, modelsFile)
}
