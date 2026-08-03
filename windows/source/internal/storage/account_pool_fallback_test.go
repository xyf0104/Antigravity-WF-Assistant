package storage

import (
	"os"
	"testing"
)

func TestAcquireAccountForModelFallsBackToDirectCredentialWhenPoolIsUnavailable(t *testing.T) {
	Init(t.TempDir())
	if err := SaveUpstreamAccount(UpstreamAccount{
		ID: "temporarily-unavailable", Name: "temporarily unavailable", Provider: "openai", Type: "api_key",
		APIURL: "https://pool.example.test", APIKey: "pool-key", AuthMode: "bearer", Enabled: false, MaxConcurrency: 1,
	}); err != nil {
		t.Fatal(err)
	}

	model := CustomModel{
		Provider: "openai", APIURL: "https://direct.example.test", APIKey: "direct-key", ExternalModelName: "gpt-direct",
		AccountIDs: []string{"temporarily-unavailable"},
	}
	selected, lease, err := AcquireAccountForModel(model, nil)
	if err != nil {
		t.Fatalf("direct fallback returned an error: %v", err)
	}
	if lease != nil {
		t.Fatalf("direct fallback reserved an unavailable account: %#v", lease)
	}
	if selected.APIKey != model.APIKey || selected.APIURL != model.APIURL {
		t.Fatalf("direct fallback changed model credentials: %#v", selected)
	}

	if err := SetUpstreamAccountEnabled("temporarily-unavailable", true); err != nil {
		t.Fatal(err)
	}
	selected, lease, err = AcquireAccountForModel(model, nil)
	if err != nil {
		t.Fatalf("healthy account did not override direct fallback: %v", err)
	}
	if lease == nil || lease.ID != "temporarily-unavailable" {
		t.Fatalf("lease = %#v, want the healthy bound account", lease)
	}
	if selected.APIKey != "pool-key" || selected.APIURL != "https://pool.example.test" {
		t.Fatalf("selected runtime model = %#v, want the healthy account route", selected)
	}
	lease.Finish(200, "", "")
}

func TestAcquireAccountForModelFallsBackToDirectCredentialWhenPoolStorageCannotBeRead(t *testing.T) {
	Init(t.TempDir())
	if err := os.WriteFile(accountsFile, []byte(`{"accounts":`), 0o600); err != nil {
		t.Fatalf("corrupt account storage for fallback test: %v", err)
	}
	model := CustomModel{
		Provider: "openai", APIURL: "https://direct.example.test", APIKey: "direct-key", ExternalModelName: "gpt-direct",
		AccountIDs: []string{"missing-or-unreadable-account"},
	}
	selected, lease, err := AcquireAccountForModel(model, nil)
	if err != nil {
		t.Fatalf("direct fallback after account storage read error: %v", err)
	}
	if lease != nil || selected.APIKey != model.APIKey || selected.APIURL != model.APIURL {
		t.Fatalf("storage-error fallback = %#v, lease = %#v", selected, lease)
	}
}
