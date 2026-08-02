package proxy

import "testing"

func TestDiagnosticsTrackModelInjection(t *testing.T) {
	InitTrace(t.TempDir())
	trace("request", map[string]any{
		"cleanPath": "/v1internal:fetchAvailableModels",
	})
	trace("models-injected", map[string]any{
		"customCount": 2,
		"containers":  []string{"models:map", "availableModels:array"},
		"customNames": []string{"First", "Second"},
		"customSlugs": []string{"custom-first", "custom-second"},
		"indexPaths":  []string{"agentModelSorts.groups[].modelIds"},
	})

	diagnostics := GetDiagnostics()
	if diagnostics.LastRequestPath != "/v1internal:fetchAvailableModels" || diagnostics.LastModelFetchAt == "" {
		t.Fatalf("model fetch was not tracked: %+v", diagnostics)
	}
	if diagnostics.LastInjectedModelCount != 2 || diagnostics.LastModelInjectionAt == "" {
		t.Fatalf("model injection was not tracked: %+v", diagnostics)
	}
	if diagnostics.LastModelShape != "models:map, availableModels:array" {
		t.Fatalf("model shape was not tracked: %+v", diagnostics)
	}
	if len(diagnostics.LastInjectedModelNames) != 2 || len(diagnostics.LastInjectedModelSlugs) != 2 || diagnostics.LastModelIndexes == "" {
		t.Fatalf("sent model details were not tracked: %+v", diagnostics)
	}

	trace("model-response-error", map[string]any{
		"statusCode": 502,
		"encoding":   "br",
		"message":    "parse failed",
	})
	diagnostics = GetDiagnostics()
	if diagnostics.LastError != "parse failed" || diagnostics.LastModelStatusCode != 502 || diagnostics.LastModelContentEncoding != "br" {
		t.Fatalf("model response failure was not tracked: %+v", diagnostics)
	}
}
