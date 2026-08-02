package storage

import (
	"os"
	"path/filepath"
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
	if info.Mode().Perm() != 0o600 {
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
