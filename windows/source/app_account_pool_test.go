package main

import (
	"testing"

	"antigravity-byok/internal/storage"
	"antigravity-byok/internal/upstream"
)

func saveModelTestAccount(t *testing.T, id, provider, token string) {
	t.Helper()
	account := storage.UpstreamAccount{
		ID: id, Name: id, Provider: provider, Type: "api_key", APIURL: "https://api.example.test",
		APIKey: token, AuthMode: "bearer", Enabled: true, Priority: 10, MaxConcurrency: 1,
	}
	if provider == "anthropic" {
		account.AuthMode = "x_api_key"
		account.APIStyle = "messages"
	}
	if err := storage.SaveUpstreamAccount(account); err != nil {
		t.Fatalf("save account %s: %v", id, err)
	}
}

func TestAccountBoundModelSavesWithoutCopiedModelCredential(t *testing.T) {
	storage.Init(t.TempDir())
	saveModelTestAccount(t, "first", "openai", "token-one")
	saveModelTestAccount(t, "second", "openai", "token-two")

	app := &App{}
	result := app.SaveModel(storage.CustomModel{
		Name: "models/account-bound", DisplayName: "account-bound", Provider: "openai", ExternalModelName: "gpt-test",
		AccountIDs: []string{"first", "second"}, APIStyle: "auto",
	})
	if !result.OK {
		t.Fatalf("account-bound model was rejected without a direct API key: %+v", result)
	}
	models, err := storage.LoadModels()
	if err != nil || len(models) != 1 {
		t.Fatalf("saved models = %#v, %v", models, err)
	}
	if len(models[0].AccountIDs) != 2 || models[0].AccountIDs[0] != "first" || models[0].AccountIDs[1] != "second" {
		t.Fatalf("model did not retain its complete account pool: %#v", models[0].AccountIDs)
	}
	if models[0].APIKey != "" {
		t.Fatal("model copied an account credential into model storage")
	}
}

func TestAccountBoundDiscoveryUsesPrimaryAndBatchImportRetainsPool(t *testing.T) {
	storage.Init(t.TempDir())
	saveModelTestAccount(t, "first", "openai", "token-one")
	saveModelTestAccount(t, "second", "openai", "token-two")

	app := &App{}
	config := upstream.Config{Provider: "openai", AccountIDs: []string{"first", "second"}}
	resolved, err := app.resolveUpstreamConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AccountID != "first" || resolved.APIKey != "token-one" {
		t.Fatalf("discovery/test did not use the first selected account: %+v", resolved)
	}

	result := app.AddDiscoveredModels(config, []string{"gpt-one", "gpt-two"})
	if !result.OK || result.Added != 2 {
		t.Fatalf("batch import failed: %+v", result)
	}
	models, err := storage.LoadModels()
	if err != nil || len(models) != 2 {
		t.Fatalf("imported models = %#v, %v", models, err)
	}
	for _, model := range models {
		if len(model.AccountIDs) != 2 || model.AccountIDs[0] != "first" || model.AccountIDs[1] != "second" {
			t.Fatalf("%s lost bound accounts: %#v", model.Name, model.AccountIDs)
		}
		if model.APIKey != "" {
			t.Fatalf("%s copied the primary account credential", model.Name)
		}
	}
}

func TestAccountPoolRejectsMixedProviders(t *testing.T) {
	storage.Init(t.TempDir())
	saveModelTestAccount(t, "openai", "openai", "token-one")
	saveModelTestAccount(t, "claude", "anthropic", "token-two")

	app := &App{}
	if _, err := app.resolveUpstreamConfig(upstream.Config{Provider: "openai", AccountIDs: []string{"openai", "claude"}}); err == nil {
		t.Fatal("mixed provider pool was accepted for discovery/test")
	}
	result := app.SaveModel(storage.CustomModel{
		Name: "models/mixed", Provider: "openai", ExternalModelName: "gpt-test", AccountIDs: []string{"openai", "claude"},
	})
	if result.OK {
		t.Fatalf("mixed provider pool was saved: %+v", result)
	}
}
