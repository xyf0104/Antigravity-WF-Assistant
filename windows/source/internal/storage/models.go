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

// DefaultCapabilities advertises only the features with a concrete conversion
// path. Audio and video are deliberately unavailable because OpenAI Chat,
// Responses and Anthropic Messages do not share a safe common attachment
// format. Hosted web search and image generation are OpenAI Responses tools,
// so they are not claimed for Claude or generic compatibility gateways.
func DefaultCapabilities(provider, modelName string) ModelCapabilities {
	return defaultCapabilities(provider, modelName, "auto")
}

// DefaultCapabilitiesForAPIStyle is used when a discovered model already has
// an explicit upstream API style. A Chat-only endpoint must not be advertised
// as supporting Responses-only hosted tools.
func DefaultCapabilitiesForAPIStyle(provider, modelName, apiStyle string) ModelCapabilities {
	return defaultCapabilities(provider, modelName, apiStyle)
}

func defaultCapabilities(provider, modelName, apiStyle string) ModelCapabilities {
	name := strings.ToLower(strings.TrimSpace(modelName))
	nonChat := strings.Contains(name, "embedding") || strings.Contains(name, "tts") || strings.Contains(name, "whisper")
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "openai"
	}
	apiStyle = strings.ToLower(strings.TrimSpace(apiStyle))
	supportsResponsesTools := !nonChat && provider == "openai" && apiStyle != "chat_completions" && apiStyle != "messages"
	capabilities := ModelCapabilities{
		SupportsImages:          !nonChat,
		SupportsFiles:           !nonChat,
		SupportsToolCalls:       !nonChat,
		SupportsThinking:        !nonChat,
		SupportsWebSearch:       supportsResponsesTools,
		SupportsImageGeneration: supportsResponsesTools,
	}
	capabilities.SupportedMimeTypes = capabilityMimeTypes(capabilities)
	return capabilities
}

func capabilityMimeTypes(capabilities ModelCapabilities) []string {
	var values []string
	if len(capabilities.SupportedMimeTypes) > 0 {
		values = capabilities.SupportedMimeTypes
	} else {
		if capabilities.SupportsImages || capabilities.SupportsFiles {
			values = append(values, CommonMediaMimeTypes...)
		}
		if capabilities.SupportsAudio {
			values = append(values, audioMimeTypes...)
		}
		if capabilities.SupportsVideo {
			values = append(values, videoMimeTypes...)
		}
	}
	normalized := normalizeMimeTypes(values)
	result := make([]string, 0, len(normalized))
	for _, mimeType := range normalized {
		if capabilityAllowsMimeType(capabilities, mimeType) {
			result = append(result, mimeType)
		}
	}
	return normalizeMimeTypes(result)
}

func capabilityAllowsMimeType(capabilities ModelCapabilities, mimeType string) bool {
	switch {
	case strings.HasPrefix(mimeType, "audio/"):
		return capabilities.SupportsAudio
	case strings.HasPrefix(mimeType, "video/"):
		return capabilities.SupportsVideo
	case strings.HasPrefix(mimeType, "image/"):
		return capabilities.SupportsImages
	default:
		return capabilities.SupportsFiles
	}
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
		capabilities = DefaultCapabilitiesForAPIStyle(model.Provider, model.ExternalModelName, model.APIStyle)
	}
	// Legacy configs could opt into audio/video even though this proxy has no
	// lossless conversion for either. Never inject an attachment MIME type that
	// the runtime cannot reliably forward.
	capabilities.SupportsAudio = false
	capabilities.SupportsVideo = false
	if !capabilitySupportsResponsesTools(model) {
		capabilities.SupportsWebSearch = false
		capabilities.SupportsImageGeneration = false
	}
	capabilities.SupportedMimeTypes = capabilityMimeTypes(capabilities)
	return capabilities
}

func capabilitySupportsResponsesTools(model CustomModel) bool {
	provider := strings.ToLower(strings.TrimSpace(model.Provider))
	if provider == "" {
		provider = "openai"
	}
	style := strings.ToLower(strings.TrimSpace(model.APIStyle))
	return provider == "openai" && style != "chat_completions" && style != "messages"
}

// CustomModel represents a third-party model configuration.
type CustomModel struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Provider    string `json:"provider"` // "openai" | "anthropic" | "grok" | "custom"
	APIKey      string `json:"apiKey"`
	APIURL      string `json:"apiUrl"`
	// EndpointMode is "auto" for a base domain/path that WF expands, or
	// "manual" to send requests to APIURL exactly as entered by the user.
	EndpointMode      string `json:"endpointMode,omitempty"`
	ExternalModelName string `json:"externalModelName"`
	ReasoningEffort   string `json:"reasoningEffort,omitempty"` // "auto" | "none" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max"
	APIStyle          string `json:"apiStyle,omitempty"`        // "auto" | "chat_completions" | "responses" | "messages"
	// MessagePathMode controls Anthropic endpoint resolution. auto first uses
	// the standard /v1/messages route; compat selects /v1/chat/messages for
	// gateways that expose that legacy-compatible shape; manual preserves the
	// path supplied in APIURL.
	MessagePathMode string            `json:"messagePathMode,omitempty"`
	AuthMode        string            `json:"authMode,omitempty"` // "bearer" | "x_api_key" | "custom_header"
	AuthHeader      string            `json:"authHeader,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	// AccountIDs binds a model to one or more reusable upstream accounts. Empty
	// preserves legacy per-model API credentials for existing installations.
	AccountIDs   []string          `json:"accountIds,omitempty"`
	Capabilities ModelCapabilities `json:"capabilities,omitempty"`
	// RuntimeOAuthUpstream and RuntimeChatGPTAccountID are copied only from the
	// account selected for an in-flight request. They are intentionally not
	// persisted in custom_models.json and can never expose an OAuth token.
	RuntimeOAuthUpstream    string `json:"-"`
	RuntimeChatGPTAccountID string `json:"-"`
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
	initAccountsFile(dir)
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
	model.EndpointMode = normalizeEndpointMode(model.EndpointMode)
	model.MessagePathMode = normalizeMessagePathMode(model.MessagePathMode)
	model.AuthMode = strings.ToLower(strings.TrimSpace(model.AuthMode))
	model.AuthHeader = strings.TrimSpace(model.AuthHeader)
	model.ExternalModelName = strings.TrimSpace(model.ExternalModelName)
	model.Name = strings.TrimSpace(model.Name)
	if strings.TrimSpace(model.DisplayName) == "" {
		model.DisplayName = strings.TrimSpace(model.ExternalModelName)
	}
	model.DisplayName = strings.TrimSpace(model.DisplayName)
	model.AccountIDs = normalizedAccountIDs(model.AccountIDs)
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
		EndpointMode:      "auto",
		MessagePathMode:   "auto",
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
