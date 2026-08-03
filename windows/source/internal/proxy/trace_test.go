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

func TestDiagnosticsTreatsCanceledModelFetchAsCancellation(t *testing.T) {
	InitTrace(t.TempDir())
	trace("model-response-error", map[string]any{
		"statusCode": 400,
		"encoding":   "gzip",
		"message":    "upstream rejected request",
	})

	trace("model-response-error", map[string]any{
		"message": "context canceled",
	})
	diagnostics := GetDiagnostics()
	if !diagnostics.LastModelRequestCanceled {
		t.Fatalf("canceled model request was not marked: %+v", diagnostics)
	}
	if diagnostics.LastError != "" || diagnostics.LastModelStatusCode != 0 || diagnostics.LastModelContentEncoding != "" {
		t.Fatalf("canceled model request retained stale error metadata: %+v", diagnostics)
	}

	// Chat-stream cancellation is unrelated to model-list injection and must
	// not overwrite the status shown on the dashboard.
	trace("openai-stream-error", map[string]any{"message": "context canceled"})
	diagnostics = GetDiagnostics()
	if !diagnostics.LastModelRequestCanceled || diagnostics.LastError != "" || diagnostics.LastModelStatusCode != 0 {
		t.Fatalf("non-model cancellation overwrote model diagnostics: %+v", diagnostics)
	}

	trace("models-injected", map[string]any{"customCount": 1})
	diagnostics = GetDiagnostics()
	if diagnostics.LastModelRequestCanceled {
		t.Fatalf("successful injection did not clear cancellation state: %+v", diagnostics)
	}
}
