package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// CustomModel represents a third-party model configuration.
type CustomModel struct {
	Name              string `json:"name"`
	DisplayName       string `json:"displayName"`
	Description       string `json:"description"`
	Provider          string `json:"provider"` // "openai" | "anthropic" | "custom"
	APIKey            string `json:"apiKey"`
	APIURL            string `json:"apiUrl"`
	ExternalModelName string `json:"externalModelName"`
	ReasoningEffort   string `json:"reasoningEffort,omitempty"` // "auto" | "none" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max"
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
	if strings.TrimSpace(model.DisplayName) == "" {
		model.DisplayName = strings.TrimSpace(model.ExternalModelName)
	}
	return model
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
