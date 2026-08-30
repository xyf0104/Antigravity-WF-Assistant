package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"antigravity-wf-assistant/internal/storage"
	"github.com/andybalholm/brotli"
)

func decodeAntigravityStreamResponse(t *testing.T, out string) map[string]any {
	t.Helper()
	payload := strings.TrimSuffix(strings.TrimPrefix(out, "data: "), "\n\n")
	var envelope map[string]any
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	response, ok := envelope["response"].(map[string]any)
	if !ok {
		t.Fatalf("missing Antigravity response envelope: %v", envelope)
	}
	if _, ok := envelope["metadata"].(map[string]any); !ok {
		t.Fatalf("missing Antigravity metadata envelope: %v", envelope)
	}
	if _, ok := envelope["traceId"].(string); !ok {
		t.Fatalf("missing Antigravity traceId envelope: %v", envelope)
	}
	return response
}

// modelFetchRoundTripper keeps fetchAvailableModels tests entirely local.
type modelFetchRoundTripper func(*http.Request) (*http.Response, error)

func (fn modelFetchRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func newModelFetchTestClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: transport}
}

func TestGetModelSlug(t *testing.T) {
	cases := []struct {
		model storage.CustomModel
		want  string
	}{
		{storage.CustomModel{ExternalModelName: "claude-fable-5"}, "custom-claude-fable-5"},
		{storage.CustomModel{ExternalModelName: "gpt-5.6-sol"}, "custom-gpt-5-6-sol"},
		{storage.CustomModel{ExternalModelName: "GPT-4o"}, "custom-gpt-4o"},
		{storage.CustomModel{Name: "models/fable5"}, "custom-fable5"},
		{storage.CustomModel{ExternalModelName: "中文模型"}, "custom-model"},
	}
	for _, c := range cases {
		got := getModelSlug(c.model)
		if got != c.want {
			t.Errorf("getModelSlug(%+v) = %q, want %q", c.model, got, c.want)
		}
	}
}

func TestGetModelPlaceholderStable(t *testing.T) {
	m := storage.CustomModel{DisplayName: "测试模型"}
	first := getModelPlaceholder(m)
	second := getModelPlaceholder(m)
	if first != second {
		t.Errorf("placeholder not stable: %q vs %q", first, second)
	}
	if !strings.HasPrefix(first, "MODEL_PLACEHOLDER_M") {
		t.Errorf("bad placeholder format: %q", first)
	}
	var placeholderNumber int
	if _, err := fmt.Sscanf(first, "MODEL_PLACEHOLDER_M%d", &placeholderNumber); err != nil || placeholderNumber < 0 || placeholderNumber >= modelPlaceholderCount {
		t.Errorf("placeholder is outside the supported enum range: %q", first)
	}
	// Different display names should differ
	other := getModelPlaceholder(storage.CustomModel{DisplayName: "鸡皮提"})
	if first == other {
		t.Errorf("different models produced same placeholder %q", first)
	}
}

func TestAddAgentModelID(t *testing.T) {
	parsed := map[string]any{
		"agentModelSorts": []any{
			map[string]any{
				"displayName": "Recommended",
				"groups": []any{
					map[string]any{"modelIds": []any{"gemini-3.6-flash-high"}},
				},
			},
		},
	}
	addAgentModelID(&parsed, "custom-test-model")

	sorts := parsed["agentModelSorts"].([]any)
	group := sorts[0].(map[string]any)["groups"].([]any)[0].(map[string]any)
	ids := group["modelIds"].([]any)

	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d: %v", len(ids), ids)
	}
	if ids[0] != "custom-test-model" {
		t.Errorf("custom model not first: %v", ids)
	}

	// Idempotent
	addAgentModelID(&parsed, "custom-test-model")
	ids = parsed["agentModelSorts"].([]any)[0].(map[string]any)["groups"].([]any)[0].(map[string]any)["modelIds"].([]any)
	if len(ids) != 2 {
		t.Errorf("duplicate insert: %v", ids)
	}
}

func TestInjectCustomModelsSupportsMapAndEverySortGroup(t *testing.T) {
	models := []storage.CustomModel{{
		Name: "models/gpt-test", DisplayName: "gpt-5.6-sol", UpstreamName: "无风", ExternalModelName: "gpt-test",
	}}
	parsed := map[string]any{
		"models": map[string]any{
			"official": map[string]any{"model": "MODEL_PLACEHOLDER_M1"},
		},
		"agentModelSorts": []any{
			map[string]any{"groups": []any{
				map[string]any{"modelIds": []any{"official"}},
				map[string]any{"modelIds": []any{"other"}},
			}},
			map[string]any{"groups": []any{
				map[string]any{"modelIds": []any{"fast"}},
			}},
		},
	}

	summary := injectCustomModels(parsed, models)
	if summary.customCount != 1 || summary.officialCount != 1 {
		t.Fatalf("unexpected injection summary: %+v", summary)
	}
	modelMap := parsed["models"].(map[string]any)
	entry, ok := modelMap["custom-gpt-test"].(map[string]any)
	if !ok || entry["displayName"] != "gpt-5.6-sol · 无风" {
		t.Fatalf("custom map entry missing: %v", modelMap)
	}
	for _, rawSort := range parsed["agentModelSorts"].([]any) {
		for _, rawGroup := range rawSort.(map[string]any)["groups"].([]any) {
			ids := rawGroup.(map[string]any)["modelIds"].([]any)
			if len(ids) == 0 || ids[0] != "custom-gpt-test" {
				t.Fatalf("custom model missing from sort group: %v", ids)
			}
		}
	}
}

func TestInjectCustomModelsSupportsArrayAndAlternateContainer(t *testing.T) {
	models := []storage.CustomModel{{
		Name: "models/claude-test", DisplayName: "gpt-5.6-sol", UpstreamName: "无风", ExternalModelName: "claude-test",
	}}
	parsed := map[string]any{
		"availableModels": []any{map[string]any{
			"name": "models/official", "displayName": "Official",
		}},
		"agentModelSorts": []any{map[string]any{"groups": []any{
			map[string]any{"modelIds": []any{"official"}},
		}}},
	}

	summary := injectCustomModels(parsed, models)
	if summary.customCount != 1 || summary.officialCount != 1 {
		t.Fatalf("unexpected injection summary: %+v", summary)
	}
	entries := parsed["availableModels"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected injected and official array models: %v", entries)
	}
	custom := entries[0].(map[string]any)
	if custom["name"] != "models/custom-claude-test" || custom["displayName"] != "gpt-5.6-sol · 无风" {
		t.Fatalf("unexpected custom array entry: %v", custom)
	}
	sorts := parsed["agentModelSorts"].([]any)
	ids := sorts[0].(map[string]any)["groups"].([]any)[0].(map[string]any)["modelIds"].([]any)
	if len(ids) != 2 || ids[0] != "custom-claude-test" || ids[1] != "official" {
		t.Fatalf("custom sort was not updated: %v", ids)
	}
}

func TestInjectCustomModelsSupportsNestedResponseIndexesAndSlugCollisions(t *testing.T) {
	models := []storage.CustomModel{
		{Name: "models/a", DisplayName: "First", ExternalModelName: "Same Model"},
		{Name: "models/b", DisplayName: "Second", ExternalModelName: "same-model"},
	}
	parsed := map[string]any{
		"response": map[string]any{
			"models": map[string]any{
				"official": map[string]any{"model": "MODEL_PLACEHOLDER_M1"},
			},
			"agentModelSorts": []any{map[string]any{"groups": []any{
				map[string]any{"modelIds": []any{"official"}},
			}}},
			"battleModeModelSorts": []any{map[string]any{"groups": []any{
				map[string]any{"modelIds": []any{"official"}},
			}}},
			"tieredModelIds": map[string]any{"recommended": []any{"official"}},
		},
	}

	summary := injectCustomModels(parsed, models)
	if summary.customCount != 2 {
		t.Fatalf("unexpected injection summary: %+v", summary)
	}
	if err := validateModelInjection(parsed, models, summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.customSlugs) != 2 || summary.customSlugs[0] == summary.customSlugs[1] {
		t.Fatalf("slug collision was not resolved: %v", summary.customSlugs)
	}
	response := parsed["response"].(map[string]any)
	modelMap := response["models"].(map[string]any)
	for _, slug := range summary.customSlugs {
		if _, ok := modelMap[slug]; !ok {
			t.Fatalf("nested model %s missing: %v", slug, modelMap)
		}
	}
	for _, key := range []string{"agentModelSorts", "battleModeModelSorts"} {
		ids := response[key].([]any)[0].(map[string]any)["groups"].([]any)[0].(map[string]any)["modelIds"].([]any)
		if len(ids) < 3 || ids[0] != summary.customSlugs[0] || ids[1] != summary.customSlugs[1] {
			t.Fatalf("%s was not updated: %v", key, ids)
		}
	}
	tiered := response["tieredModelIds"].(map[string]any)["recommended"].([]any)
	if len(tiered) < 3 || tiered[0] != summary.customSlugs[0] || tiered[1] != summary.customSlugs[1] {
		t.Fatalf("tiered model index was not updated: %v", tiered)
	}
	if len(summary.containers) != 1 || summary.containers[0] != "response.models:map" {
		t.Fatalf("nested response path was not diagnosed: %v", summary.containers)
	}
}

func TestInjectCustomModelsAddsOnlyDirectImageModelsToImageGenerationIndex(t *testing.T) {
	imageModel := storage.CustomModel{
		Name: "models/image", DisplayName: "Image model", ExternalModelName: "gpt-image-2",
		Provider: "openai", APIStyle: "responses",
		Capabilities: storage.ModelCapabilities{
			Configured: true, SupportsImages: true, SupportsFiles: true,
			SupportsToolCalls: true, SupportsImageGeneration: true,
		},
	}
	textModel := storage.CustomModel{
		Name: "models/text", DisplayName: "Text model", ExternalModelName: "gpt-text",
		Provider: "openai", APIStyle: "chat_completions",
		Capabilities: storage.ModelCapabilities{
			Configured: true, SupportsImages: true, SupportsFiles: true, SupportsToolCalls: true,
		},
	}
	parsed := map[string]any{
		"models": map[string]any{
			"native-image": map[string]any{"model": "MODEL_PLACEHOLDER_M1"},
		},
		"agentModelSorts": []any{map[string]any{
			"groups": []any{map[string]any{"modelIds": []any{"native-image"}}},
		}},
		"imageGenerationModelIds": []any{"native-image"},
	}

	summary := injectCustomModels(parsed, []storage.CustomModel{imageModel, textModel})
	if err := validateModelInjection(parsed, []storage.CustomModel{imageModel, textModel}, summary); err != nil {
		t.Fatal(err)
	}

	imageSlug := summary.assignments.slugs[modelPlaceholderKey(imageModel)]
	textSlug := summary.assignments.slugs[modelPlaceholderKey(textModel)]
	imageIDs := parsed["imageGenerationModelIds"].([]any)
	if len(imageIDs) != 2 || imageIDs[0] != imageSlug || imageIDs[1] != "native-image" {
		t.Fatalf("image-generation index = %#v, want only %q plus native model", imageIDs, imageSlug)
	}
	for _, id := range imageIDs {
		if id == textSlug {
			t.Fatalf("text-only model leaked into image-generation index: %#v", imageIDs)
		}
	}
	if got := summary.assignments.nativeImageModelIDs; len(got) != 1 || got[0] != "native-image" {
		t.Fatalf("native image model directory was not captured before custom injection: %#v", got)
	}

	models := parsed["models"].(map[string]any)
	if models[imageSlug].(map[string]any)["requiresImageOutputOutsideFunctionResponses"] != true {
		t.Fatalf("image model is missing image-output presentation capability: %#v", models[imageSlug])
	}
	if models[textSlug].(map[string]any)["requiresImageOutputOutsideFunctionResponses"] != true {
		t.Fatalf("OpenAI Chat model must expose the proxy's dedicated Images bridge: %#v", models[textSlug])
	}
	imageIndexDiagnosed := false
	for _, path := range summary.indexPaths {
		if path == "imageGenerationModelIds" {
			imageIndexDiagnosed = true
			break
		}
	}
	if !imageIndexDiagnosed {
		t.Fatalf("image-generation index update was not diagnosed: %#v", summary.indexPaths)
	}
}

func TestInjectCustomModelsDoesNotCreateUnknownImageGenerationIndex(t *testing.T) {
	model := storage.CustomModel{
		Name: "models/image", DisplayName: "Image model", ExternalModelName: "gpt-image-2",
		Provider: "openai", APIStyle: "responses",
		Capabilities: storage.ModelCapabilities{Configured: true, SupportsImageGeneration: true},
	}
	parsed := map[string]any{"models": map[string]any{}}
	injectCustomModels(parsed, []storage.CustomModel{model})
	if _, exists := parsed["imageGenerationModelIds"]; exists {
		t.Fatalf("older model response unexpectedly gained an image-generation index: %#v", parsed)
	}
}

func TestValidateModelInjectionRejectsMissingPickerIndex(t *testing.T) {
	models := []storage.CustomModel{{Name: "models/test", DisplayName: "Test", ExternalModelName: "test"}}
	parsed := map[string]any{"models": map[string]any{}}
	summary := injectCustomModels(parsed, models)
	if _, exists := parsed["agentModelSorts"]; exists {
		t.Fatalf("injection invented agentModelSorts for an unknown picker protocol: %#v", parsed)
	}
	if err := validateModelInjection(parsed, models, summary); err == nil {
		t.Fatal("expected validation failure for model without a picker index")
	}
}

func TestDecodeModelResponseEncodings(t *testing.T) {
	payload := []byte(`{"models":{},"agentModelSorts":[{"groups":[{"modelIds":[]}]}]}`)
	encoders := map[string]func(*bytes.Buffer) io.WriteCloser{
		"gzip": func(buffer *bytes.Buffer) io.WriteCloser { return gzip.NewWriter(buffer) },
		"deflate": func(buffer *bytes.Buffer) io.WriteCloser {
			writer := zlib.NewWriter(buffer)
			return writer
		},
		"raw-deflate": func(buffer *bytes.Buffer) io.WriteCloser {
			writer, err := flate.NewWriter(buffer, flate.DefaultCompression)
			if err != nil {
				t.Fatal(err)
			}
			return writer
		},
		"br": func(buffer *bytes.Buffer) io.WriteCloser { return brotli.NewWriter(buffer) },
	}
	for name, makeWriter := range encoders {
		var encoded bytes.Buffer
		writer := makeWriter(&encoded)
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		encoding := name
		if name == "raw-deflate" {
			encoding = "deflate"
		}
		decoded, err := decodeModelResponse(encoded.Bytes(), encoding)
		if err != nil || !bytes.Equal(decoded, payload) {
			t.Fatalf("%s decode = %q, %v", name, decoded, err)
		}
	}
	if _, err := decodeModelResponse(payload, "zstd"); err == nil {
		t.Fatal("expected unsupported encoding error")
	}
}

func TestHandleFetchAvailableModelsInjectsIntoCompressedJSONWithoutChangingNativeModels(t *testing.T) {
	stateDir := t.TempDir()
	storage.Init(stateDir)
	InitTrace(stateDir)
	custom := storage.CustomModel{
		Name: "models/gpt-wf", DisplayName: "GPT WF", Description: "custom upstream",
		Provider: "openai", ExternalModelName: "gpt-wf",
	}
	if err := storage.SaveModels([]storage.CustomModel{custom}); err != nil {
		t.Fatal(err)
	}

	native := map[string]any{
		"name":             "models/native-gemini",
		"displayName":      "Native Gemini",
		"supportsImages":   true,
		"nativeCapability": map[string]any{"keep": true},
	}
	payload, err := json.Marshal(map[string]any{
		"response": map[string]any{
			"availableModels": []any{native},
			"agentModelSorts": []any{map[string]any{
				"displayName": "Native",
				"groups":      []any{map[string]any{"modelIds": []any{"native-gemini"}}},
			}},
			"imageGenerationModelIds": []any{"native-gemini"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	client := newModelFetchTestClient(modelFetchRoundTripper(func(request *http.Request) (*http.Response, error) {
		if got, want := request.URL.String(), googleBaseURL+"/v1internal:fetchAvailableModels"; got != want {
			t.Fatalf("upstream URL = %q, want %q", got, want)
		}
		if got := request.Header.Get("Accept-Encoding"); got != "identity" {
			t.Fatalf("upstream Accept-Encoding = %q, want identity", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":     []string{"application/json"},
				"Content-Encoding": []string{"gzip"},
				"X-Native-Model":   []string{"preserved"},
			},
			Body:    io.NopCloser(bytes.NewReader(compressed.Bytes())),
			Request: request,
		}, nil
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1internal:fetchAvailableModels", strings.NewReader(`{"client":"antigravity"}`))
	handleFetchAvailableModelsWithClient(recorder, request, client)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("decoded response must not retain Content-Encoding, got %q", got)
	}
	if got := recorder.Header().Get("X-Native-Model"); got != "preserved" {
		t.Fatalf("native response header = %q", got)
	}

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	root := response["response"].(map[string]any)
	entries := root["availableModels"].([]any)
	if len(entries) != 2 {
		t.Fatalf("availableModels count = %d, want custom + native: %#v", len(entries), entries)
	}
	injected := entries[0].(map[string]any)
	if injected["name"] != "models/custom-gpt-wf" || injected["displayName"] != "GPT WF" {
		t.Fatalf("unexpected injected model: %#v", injected)
	}
	if injected["supportsImages"] != true || injected["supportsAudio"] != false || injected["supportsVideo"] != false || injected["supportsToolCalls"] != true {
		t.Fatalf("custom model capability declaration is incomplete: %#v", injected)
	}
	if injected["requiresImageOutputOutsideFunctionResponses"] != true {
		t.Fatalf("image-capable model is missing image-output presentation capability: %#v", injected)
	}
	nativeAfter := entries[1].(map[string]any)
	if nativeAfter["displayName"] != "Native Gemini" || nativeAfter["nativeCapability"].(map[string]any)["keep"] != true {
		t.Fatalf("native model was modified: %#v", nativeAfter)
	}
	ids := root["agentModelSorts"].([]any)[0].(map[string]any)["groups"].([]any)[0].(map[string]any)["modelIds"].([]any)
	if len(ids) != 2 || ids[0] != "custom-gpt-wf" || ids[1] != "native-gemini" {
		t.Fatalf("picker indexes = %#v", ids)
	}
	imageIDs := root["imageGenerationModelIds"].([]any)
	if len(imageIDs) != 1 || imageIDs[0] != "native-gemini" {
		t.Fatalf("image-generation indexes = %#v", imageIDs)
	}

	diagnostics := GetDiagnostics()
	if diagnostics.LastInjectedModelCount != 1 || diagnostics.LastModelShape != "response.availableModels:array" || diagnostics.LastError != "" {
		t.Fatalf("unexpected model diagnostics: %+v", diagnostics)
	}
}

func TestHandleFetchAvailableModelsForwardsUnusableUpstreamResponsesUntouched(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		encoding string
		body     []byte
	}{
		{name: "upstream-error", status: http.StatusTooManyRequests, encoding: "gzip", body: []byte(`quota exhausted`)},
		{name: "invalid-json", status: http.StatusOK, body: []byte(`{"models":`)},
		{name: "invalid-gzip", status: http.StatusOK, encoding: "gzip", body: []byte(`not a gzip stream`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			storage.Init(stateDir)
			InitTrace(stateDir)
			if err := storage.SaveModels([]storage.CustomModel{{Name: "models/should-not-appear", DisplayName: "Should not appear", ExternalModelName: "should-not-appear"}}); err != nil {
				t.Fatal(err)
			}

			client := newModelFetchTestClient(modelFetchRoundTripper(func(request *http.Request) (*http.Response, error) {
				header := http.Header{"Content-Type": []string{"application/json"}, "X-Upstream-Error": []string{"kept"}}
				if test.encoding != "" {
					header.Set("Content-Encoding", test.encoding)
				}
				return &http.Response{
					StatusCode: test.status,
					Header:     header,
					Body:       io.NopCloser(bytes.NewReader(test.body)),
					Request:    request,
				}, nil
			}))

			recorder := httptest.NewRecorder()
			handleFetchAvailableModelsWithClient(recorder, httptest.NewRequest(http.MethodPost, "/v1internal:fetchAvailableModels", nil), client)

			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
			if !bytes.Equal(recorder.Body.Bytes(), test.body) {
				t.Fatalf("raw upstream body changed: got %q, want %q", recorder.Body.Bytes(), test.body)
			}
			if got := recorder.Header().Get("X-Upstream-Error"); got != "kept" {
				t.Fatalf("upstream header = %q", got)
			}
			if got := recorder.Header().Get("Content-Encoding"); got != test.encoding {
				t.Fatalf("content encoding = %q, want %q", got, test.encoding)
			}
			diagnostics := GetDiagnostics()
			if diagnostics.LastError == "" || diagnostics.LastModelStatusCode != test.status {
				t.Fatalf("unusable response was not diagnosed: %+v", diagnostics)
			}
		})
	}
}

func TestDisabledModelsAreNotInjectedOrRoutable(t *testing.T) {
	stateDir := t.TempDir()
	storage.Init(stateDir)
	InitTrace(stateDir)
	disabled := false
	models := []storage.CustomModel{
		{Name: "models/enabled", DisplayName: "Enabled", ExternalModelName: "enabled"},
		{Name: "models/disabled", DisplayName: "Disabled", ExternalModelName: "disabled", Enabled: &disabled},
	}
	if err := storage.SaveModels(models); err != nil {
		t.Fatal(err)
	}
	if found := findModel("models/disabled"); found != nil {
		t.Fatalf("disabled model remained routable: %#v", found)
	}
	if found := findModel("models/enabled"); found == nil {
		t.Fatal("enabled model was not routable")
	}

	payload := []byte(`{"models":{},"agentModelSorts":[{"groups":[{"modelIds":[]}]}]}`)
	client := newModelFetchTestClient(modelFetchRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	}))
	recorder := httptest.NewRecorder()
	handleFetchAvailableModelsWithClient(recorder, httptest.NewRequest(http.MethodPost, "/v1internal:fetchAvailableModels", nil), client)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "disabled") || !strings.Contains(recorder.Body.String(), "enabled") {
		t.Fatalf("disabled model leaked into picker: %s", recorder.Body.String())
	}
}

func TestJSONShapeRedactsValues(t *testing.T) {
	shape, err := json.Marshal(jsonShape(map[string]any{
		"displayName": "secret model name",
		"model":       "MODEL_PLACEHOLDER_M42",
		"recommended": true,
	}, 0))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(shape, []byte("secret model name")) || bytes.Contains(shape, []byte("M42")) {
		t.Fatalf("shape leaked values: %s", shape)
	}
}

func TestAllocateModelPlaceholdersAvoidsOfficialAndCustomCollisions(t *testing.T) {
	models := []storage.CustomModel{
		{Name: "first", DisplayName: "Same seed"},
		{Name: "second", DisplayName: "Same seed"},
	}
	official := map[string]any{
		"official": map[string]any{"model": getModelPlaceholder(models[0])},
	}
	assignments := allocateModelPlaceholders(models, official)
	first := assignments["first"]
	second := assignments["second"]
	if first == "" || second == "" {
		t.Fatalf("missing assignments: %v", assignments)
	}
	if first == second {
		t.Fatalf("custom models collided on %q", first)
	}
	if first == official["official"].(map[string]any)["model"] || second == official["official"].(map[string]any)["model"] {
		t.Fatalf("custom model collided with official model: %v", assignments)
	}
}

func TestToOpenAIRequestBasic(t *testing.T) {
	gemini := map[string]any{
		"systemInstruction": map[string]any{
			"parts": []any{map[string]any{"text": "You are helpful."}},
		},
		"contents": []any{
			map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": "Hello"}},
			},
			map[string]any{
				"role":  "model",
				"parts": []any{map[string]any{"text": "Hi there"}},
			},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": 4096,
			"temperature":     0.7,
		},
	}

	out := toOpenAIRequest(gemini, "gpt-5.6-sol")

	if out["model"] != "gpt-5.6-sol" {
		t.Errorf("model = %v", out["model"])
	}
	if out["max_tokens"] != 4096 {
		t.Errorf("max_tokens = %v", out["max_tokens"])
	}

	msgs := out["messages"].([]map[string]any)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "You are helpful." {
		t.Errorf("system message wrong: %+v", msgs[0])
	}
	if msgs[1]["role"] != "user" || msgs[1]["content"] != "Hello" {
		t.Errorf("user message wrong: %+v", msgs[1])
	}
	if msgs[2]["role"] != "assistant" {
		t.Errorf("model role not mapped to assistant: %+v", msgs[2])
	}
}

func TestToOpenAIRequestTools(t *testing.T) {
	gemini := map[string]any{
		"contents": []any{},
		"tools": []any{
			map[string]any{
				"functionDeclarations": []any{
					map[string]any{
						"name":        "read_file",
						"description": "Read a file",
						"parameters": map[string]any{
							"type": "OBJECT",
							"properties": map[string]any{
								"path":  map[string]any{"type": "STRING"},
								"wait":  map[string]any{"type": "BOOLEAN"},
								"lines": map[string]any{"type": "ARRAY", "items": map[string]any{"type": "INTEGER"}},
							},
							"required": []any{"path"},
						},
					},
				},
			},
		},
	}

	out := toOpenAIRequest(gemini, "gpt-4o")
	tools := out["tools"].([]map[string]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	fn := tools[0]["function"].(map[string]any)
	if fn["name"] != "read_file" {
		t.Errorf("tool name = %v", fn["name"])
	}
	if tools[0]["type"] != "function" {
		t.Errorf("tool type = %v", tools[0]["type"])
	}
	params := fn["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Fatalf("root schema type was not normalized: %v", params)
	}
	properties := params["properties"].(map[string]any)
	if properties["path"].(map[string]any)["type"] != "string" || properties["wait"].(map[string]any)["type"] != "boolean" {
		t.Fatalf("nested schema types were not normalized: %v", properties)
	}
	items := properties["lines"].(map[string]any)["items"].(map[string]any)
	if items["type"] != "integer" {
		t.Fatalf("array item schema type was not normalized: %v", items)
	}
}

func TestResolveOpenAIChatCompletionsURL(t *testing.T) {
	cases := map[string]string{
		"https://api.xiass.com":                    "https://api.xiass.com/v1/chat/completions",
		"https://api.xiass.com/":                   "https://api.xiass.com/v1/chat/completions",
		"https://api.xiass.com/v1":                 "https://api.xiass.com/v1/chat/completions",
		"https://example.com/openai/v1":            "https://example.com/openai/v1/chat/completions",
		"https://example.com/v1/chat/completions":  "https://example.com/v1/chat/completions",
		"https://example.com/proxy?workspace=test": "https://example.com/proxy/chat/completions?workspace=test",
	}
	for input, want := range cases {
		if got := resolveOpenAIChatCompletionsURL(input); got != want {
			t.Errorf("resolveOpenAIChatCompletionsURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestConvertOpenAILineToGeminiText(t *testing.T) {
	state := &openAIStreamState{}
	line := `data: {"choices":[{"delta":{"content":"Hello world"},"finish_reason":null}]}`

	out := convertOpenAILineToGemini(line, state)
	if out == "" {
		t.Fatal("empty conversion")
	}
	if !strings.HasPrefix(out, "data: ") {
		t.Errorf("missing SSE prefix: %q", out)
	}

	response := decodeAntigravityStreamResponse(t, out)
	cands := response["candidates"].([]any)
	content := cands[0].(map[string]any)["content"].(map[string]any)
	if content["role"] != "model" {
		t.Errorf("role = %v", content["role"])
	}
	parts := content["parts"].([]any)
	if parts[0].(map[string]any)["text"] != "Hello world" {
		t.Errorf("text = %v", parts[0])
	}
}

func TestConvertOpenAIToolCallAccumulation(t *testing.T) {
	state := &openAIStreamState{}

	// Tool call arrives in fragments
	convertOpenAILineToGemini(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_read_1","function":{"name":"read_file","arguments":"{\"pa"}}]},"finish_reason":null}]}`, state)
	convertOpenAILineToGemini(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.txt\"}"}}]},"finish_reason":null}]}`, state)
	out := convertOpenAILineToGemini(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`, state)

	if out == "" {
		t.Fatal("no output on finish")
	}
	response := decodeAntigravityStreamResponse(t, out)
	parts := response["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)
	fc := parts[0].(map[string]any)["functionCall"].(map[string]any)
	if fc["name"] != "read_file" {
		t.Errorf("function name = %v", fc["name"])
	}
	if fc["id"] != "call_read_1" {
		t.Errorf("function call id = %v", fc["id"])
	}
	args := fc["args"].(map[string]any)
	if args["path"] != "a.txt" {
		t.Errorf("args not accumulated correctly: %v", args)
	}
}

func TestConvertOpenAIUsageCapture(t *testing.T) {
	state := &openAIStreamState{}
	convertOpenAILineToGemini(`data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50}}`, state)

	if state.usage == nil {
		t.Fatal("usage not captured")
	}
	if state.usage["prompt_tokens"] != float64(100) {
		t.Errorf("prompt_tokens = %v", state.usage["prompt_tokens"])
	}
}

func TestConvertOpenAIStopProducesFinalEnvelope(t *testing.T) {
	state := &openAIStreamState{traceID: "request-123"}
	out := convertOpenAILineToGemini(`data: {"id":"chatcmpl-1","model":"gpt-test","choices":[{"delta":{},"finish_reason":"stop"}]}`, state)
	if out == "" {
		t.Fatal("stop chunk must not be discarded")
	}
	response := decodeAntigravityStreamResponse(t, out)
	candidate := response["candidates"].([]any)[0].(map[string]any)
	if candidate["finishReason"] != "STOP" {
		t.Fatalf("finishReason = %v", candidate["finishReason"])
	}
	if response["responseId"] != "chatcmpl-1" || response["modelVersion"] != "gpt-test" {
		t.Fatalf("upstream identity was not preserved: %v", response)
	}
}

func TestConvertOpenAIDoneAndEmpty(t *testing.T) {
	state := &openAIStreamState{}
	if out := convertOpenAILineToGemini("data: [DONE]", state); out != "" {
		t.Errorf("[DONE] should produce nothing, got %q", out)
	}
	if out := convertOpenAILineToGemini("", state); out != "" {
		t.Errorf("empty line should produce nothing")
	}
	if out := convertOpenAILineToGemini("event: ping", state); out != "" {
		t.Errorf("non-data line should produce nothing")
	}
}

func TestStreamOpenAIRejectsUnrecognizedSuccessfulBody(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader("<!doctype html><title>API dashboard</title>")),
	}
	recorder := httptest.NewRecorder()
	streamOpenAIResponse(recorder, resp, "request-empty", 1)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "invalid_upstream_stream") {
		t.Fatalf("unexpected error body: %s", recorder.Body.String())
	}
}

func TestStreamOpenAIAddsStopWhenUpstreamOmitsIt(t *testing.T) {
	body := `data: {"id":"chatcmpl-1","model":"gpt-test","choices":[{"delta":{"content":"hello"},"finish_reason":null}]}` + "\n\n"
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}
	recorder := httptest.NewRecorder()
	streamOpenAIResponse(recorder, resp, "request-no-stop", 1)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"finishReason":"STOP"`) {
		t.Fatalf("synthetic STOP was not appended: %s", recorder.Body.String())
	}
}

func TestToAnthropicRequestCacheBreakpoints(t *testing.T) {
	gemini := map[string]any{
		"systemInstruction": map[string]any{
			"parts": []any{map[string]any{"text": "System prompt"}},
		},
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Q1"}}},
			map[string]any{"role": "model", "parts": []any{map[string]any{"text": "A1"}}},
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Q2"}}},
		},
	}

	out := toAnthropicRequest(gemini, "claude-sonnet-4-6")
	breakpoints := applyAnthropicPromptCaching(out)
	if breakpoints < 2 || breakpoints > 4 {
		t.Fatalf("unexpected breakpoint count: %d", breakpoints)
	}

	// System should have cache_control
	sys := out["system"].([]any)
	sysBlock := sys[0].(map[string]any)
	if sysBlock["cache_control"] == nil {
		t.Error("system missing cache_control")
	}

	// Count total ephemeral breakpoints (Anthropic max 4)
	count := 0
	if sysBlock["cache_control"] != nil {
		count++
	}
	msgs := out["messages"].([]map[string]any)
	for _, m := range msgs {
		blocks := m["content"].([]any)
		for _, b := range blocks {
			if bm, ok := b.(map[string]any); ok && bm["cache_control"] != nil {
				count++
			}
		}
	}
	if count > 4 {
		t.Errorf("too many cache breakpoints: %d (Anthropic max 4)", count)
	}
	if count < 2 {
		t.Errorf("expected at least system + 1 message breakpoint, got %d", count)
	}
}

func TestToAnthropicRoleMapping(t *testing.T) {
	gemini := map[string]any{
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "question"}}},
			map[string]any{"role": "model", "parts": []any{map[string]any{"text": "response"}}},
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "follow up"}}},
		},
	}
	out := toAnthropicRequest(gemini, "claude-x")
	msgs := out["messages"].([]map[string]any)
	if msgs[1]["role"] != "assistant" {
		t.Errorf("model should map to assistant, got %v", msgs[1]["role"])
	}
}

func TestAnthropicToolConversion(t *testing.T) {
	gemini := map[string]any{
		"contents": []any{},
		"tools": []any{
			map[string]any{
				"functionDeclarations": []any{
					map[string]any{
						"name":        "grep",
						"description": "Search",
						"parameters": map[string]any{
							"type":       "OBJECT",
							"properties": map[string]any{"fixed": map[string]any{"type": "BOOLEAN"}},
						},
					},
				},
			},
		},
	}
	out := toAnthropicRequest(gemini, "claude-x")
	tools := out["tools"].([]map[string]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0]["name"] != "grep" {
		t.Errorf("name = %v", tools[0]["name"])
	}
	if tools[0]["input_schema"] == nil {
		t.Error("missing input_schema (Anthropic format)")
	}
	schema := tools[0]["input_schema"].(map[string]any)
	if schema["type"] != "object" || schema["properties"].(map[string]any)["fixed"].(map[string]any)["type"] != "boolean" {
		t.Fatalf("Anthropic schema types were not normalized: %v", schema)
	}
}

func TestConvertAnthropicTextDelta(t *testing.T) {
	state := &anthropicStreamState{}
	out := convertAnthropicLineToGemini(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hi"}}`, state)

	if out == "" {
		t.Fatal("empty conversion")
	}
	response := decodeAntigravityStreamResponse(t, out)
	parts := response["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)
	if parts[0].(map[string]any)["text"] != "Hi" {
		t.Errorf("text = %v", parts[0])
	}
}

func TestConvertAnthropicToolUse(t *testing.T) {
	state := &anthropicStreamState{}

	convertAnthropicLineToGemini(`data: {"type":"content_block_start","content_block":{"type":"tool_use","id":"toolu_1","name":"view_file"}}`, state)
	convertAnthropicLineToGemini(`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"path\""}}`, state)
	convertAnthropicLineToGemini(`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":":\"x.go\"}"}}`, state)
	out := convertAnthropicLineToGemini(`data: {"type":"content_block_stop"}`, state)

	if out == "" {
		t.Fatal("no output on content_block_stop")
	}
	response := decodeAntigravityStreamResponse(t, out)
	parts := response["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)
	fc := parts[0].(map[string]any)["functionCall"].(map[string]any)

	if fc["name"] != "view_file" {
		t.Errorf("name = %v", fc["name"])
	}
	if fc["id"] != "toolu_1" {
		t.Errorf("tool id = %v", fc["id"])
	}
	args := fc["args"].(map[string]any)
	if args["path"] != "x.go" {
		t.Errorf("args = %v", args)
	}
}

func TestConvertAnthropicToolUseWithInlineInput(t *testing.T) {
	state := &anthropicStreamState{traceID: "inline-tool"}
	convertAnthropicLineToGemini(`data: {"type":"content_block_start","content_block":{"type":"tool_use","id":"toolu_inline","name":"search","input":{"query":"天气"}}}`, state)
	out := convertAnthropicLineToGemini(`data: {"type":"content_block_stop"}`, state)
	if out == "" {
		t.Fatal("inline tool input was discarded")
	}
	response := decodeAntigravityStreamResponse(t, out)
	parts := response["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)
	args := parts[0].(map[string]any)["functionCall"].(map[string]any)["args"].(map[string]any)
	if args["query"] != "天气" {
		t.Fatalf("inline tool arguments = %#v", args)
	}
}

func TestConvertAnthropicMalformedToolUseDoesNotEmitFunctionCall(t *testing.T) {
	state := &anthropicStreamState{traceID: "malformed-tool"}
	convertAnthropicLineToGemini(`data: {"type":"content_block_start","content_block":{"type":"tool_use","id":"toolu_bad","name":"search"}}`, state)
	if out := convertAnthropicLineToGemini(`data: {"type":"content_block_stop"}`, state); out != "" {
		response := decodeAntigravityStreamResponse(t, out)
		parts := response["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)
		for _, part := range parts {
			if _, isCall := part.(map[string]any)["functionCall"]; isCall {
				t.Fatalf("malformed tool call was emitted: %#v", part)
			}
		}
	}
	stop := convertAnthropicLineToGemini(`data: {"type":"message_stop"}`, state)
	if stop == "" || !strings.Contains(stop, `"finishReason":"STOP"`) {
		t.Fatalf("malformed tool stream did not terminate cleanly: %s", stop)
	}
}

func TestConvertOpenAIInvalidToolArgumentsDoesNotEmitFunctionCall(t *testing.T) {
	state := &openAIStreamState{traceID: "invalid-openai-tool"}
	convertOpenAILineToGemini(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_bad","function":{"name":"search","arguments":"not-json"}}]},"finish_reason":null}]}`, state)
	out := convertOpenAILineToGemini(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`, state)
	if out == "" {
		t.Fatal("tool finish envelope should still be emitted")
	}
	if strings.Contains(out, "functionCall") || state.unsafeOutput {
		t.Fatalf("invalid OpenAI tool arguments were emitted: %s", out)
	}
}

func TestConvertAnthropicParallelToolUsePreservesEachID(t *testing.T) {
	state := &anthropicStreamState{}
	convertAnthropicLineToGemini(`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_first","name":"first"}}`, state)
	convertAnthropicLineToGemini(`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_second","name":"second"}}`, state)
	convertAnthropicLineToGemini(`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"value\":2}"}}`, state)
	convertAnthropicLineToGemini(`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"value\":1}"}}`, state)

	second := decodeAntigravityStreamResponse(t, convertAnthropicLineToGemini(`data: {"type":"content_block_stop","index":1}`, state))
	secondCall := second["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if secondCall["id"] != "toolu_second" || secondCall["name"] != "second" || secondCall["args"].(map[string]any)["value"] != float64(2) {
		t.Fatalf("second parallel tool call was corrupted: %#v", secondCall)
	}
	first := decodeAntigravityStreamResponse(t, convertAnthropicLineToGemini(`data: {"type":"content_block_stop","index":0}`, state))
	firstCall := first["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if firstCall["id"] != "toolu_first" || firstCall["name"] != "first" || firstCall["args"].(map[string]any)["value"] != float64(1) {
		t.Fatalf("first parallel tool call was corrupted: %#v", firstCall)
	}
}

func TestConvertAnthropicStopProducesFinalEnvelope(t *testing.T) {
	state := &anthropicStreamState{traceID: "request-456"}
	convertAnthropicLineToGemini(`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-test"}}`, state)
	out := convertAnthropicLineToGemini(`data: {"type":"message_stop"}`, state)
	response := decodeAntigravityStreamResponse(t, out)
	candidate := response["candidates"].([]any)[0].(map[string]any)
	if candidate["finishReason"] != "STOP" {
		t.Fatalf("finishReason = %v", candidate["finishReason"])
	}
	if response["responseId"] != "msg_1" || response["modelVersion"] != "claude-test" {
		t.Fatalf("upstream identity was not preserved: %v", response)
	}
}

func TestStreamAnthropicRejectsUnrecognizedSuccessfulBody(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"unexpected":"shape"}`)),
	}
	recorder := httptest.NewRecorder()
	streamAnthropicResponse(recorder, resp, "request-empty-anthropic", 1)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "invalid_upstream_stream") {
		t.Fatalf("unexpected error body: %s", recorder.Body.String())
	}
}

func TestCollectAnthropicUsage(t *testing.T) {
	totals := anthropicUsageTotals{}

	collectAnthropicUsage(`data: {"type":"message_start","message":{"usage":{"input_tokens":1000,"cache_read_input_tokens":800,"cache_creation_input_tokens":50}}}`, &totals)
	collectAnthropicUsage(`data: {"type":"message_delta","usage":{"output_tokens":200}}`, &totals)

	if !totals.seen {
		t.Fatal("usage not seen")
	}
	if totals.input != 1000 {
		t.Errorf("input = %d", totals.input)
	}
	if totals.output != 200 {
		t.Errorf("output = %d", totals.output)
	}
	if totals.cacheRead != 800 {
		t.Errorf("cacheRead = %d", totals.cacheRead)
	}
	if totals.cacheWrite != 50 {
		t.Errorf("cacheWrite = %d", totals.cacheWrite)
	}
}

func TestBuildFakeModelEntry(t *testing.T) {
	m := storage.CustomModel{
		DisplayName:       "测试模型",
		AccountPoolLabel:  "XIASS主池",
		Description:       "test",
		ExternalModelName: "claude-fable-5",
	}
	placeholder := getModelPlaceholder(m)
	entry := buildFakeModelEntry(m, placeholder)

	if entry["displayName"] != "测试模型 · XIASS主池" {
		t.Errorf("displayName = %v", entry["displayName"])
	}
	if entry["apiProvider"] != "API_PROVIDER_GOOGLE_GEMINI" {
		t.Errorf("apiProvider must mimic Google for IDE compatibility, got %v", entry["apiProvider"])
	}
	if entry["model"] != placeholder {
		t.Errorf("model placeholder mismatch")
	}
	if entry["recommended"] != true {
		t.Error("should be recommended so it surfaces in menu")
	}
	if entry["requiresImageOutputOutsideFunctionResponses"] != true {
		t.Errorf("image-capable model must request media output outside function responses: %#v", entry)
	}
}

func TestReasoningBudget(t *testing.T) {
	cases := map[string]int{
		"":       0,
		"auto":   0,
		"low":    1024,
		"medium": 4096,
		"high":   8192,
	}
	for effort, want := range cases {
		if got := reasoningBudget(effort); got != want {
			t.Errorf("reasoningBudget(%q) = %d, want %d", effort, got, want)
		}
	}
}

func TestRetryableStatusIncludesCloudflareTimeout(t *testing.T) {
	for _, code := range []int{429, 502, 503, 504, 524} {
		if !isRetryableStatus(code) {
			t.Errorf("status %d should be retryable", code)
		}
	}
	for _, code := range []int{400, 401, 500} {
		if isRetryableStatus(code) {
			t.Errorf("status %d should not be retryable", code)
		}
	}
}

func TestRetryDelay(t *testing.T) {
	if got := retryDelay(1, "2"); got != 2*time.Second {
		t.Errorf("numeric Retry-After delay = %v", got)
	}
	if got := retryDelay(1, "30"); got != 10*time.Second {
		t.Errorf("Retry-After cap = %v", got)
	}
	if got := retryDelay(2, ""); got != 500*time.Millisecond {
		t.Errorf("exponential delay = %v", got)
	}
}

func TestForwardOpenAIChatDoesNotReplayAfterPartialStreamDisconnect(t *testing.T) {
	previous := currentStreamRecoveryPolicy()
	ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: true, MaxAttempts: 2, MaxDelaySeconds: 1})
	defer ConfigureStreamRecovery(storage.StreamRecoverySettings{
		Enabled: previous.enabled, MaxAttempts: previous.maxAttempts, MaxDelaySeconds: previous.maxDelaySeconds,
	})

	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			_, _ = io.WriteString(w, "data: {\"id\":\"first\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"第一段\"},\"finish_reason\":null}]}\n\n")
			return // Simulate an upstream connection that ends before STOP.
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"second\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"第二段\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()

	model := &storage.CustomModel{Provider: "openai", APIURL: upstream.URL + "/v1", APIKey: "test-key", ExternalModelName: "gpt-test"}
	request := httptest.NewRequest(http.MethodPost, "/v1internal:streamGenerateContent", nil)
	recorder := httptest.NewRecorder()
	gemini := map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "请回答"}}}}}

	forwardOpenAIChat(recorder, request, model, gemini, "reconnect-test")

	if requests != 1 {
		t.Fatalf("upstream requests = %d, want 1: a partial answer must never be replayed with injected chat context", requests)
	}
	output := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("downstream status = %d, body = %s", recorder.Code, output)
	}
	if !strings.Contains(output, "第一段") || strings.Contains(output, "第二段") {
		t.Fatalf("partial output should be preserved without a replay: %s", output)
	}
	if !strings.Contains(output, `"finishReason":"STOP"`) {
		t.Fatalf("partial stream has no final stop: %s", output)
	}
	if strings.Contains(output, "上游连接已自动重连") || strings.Contains(output, "上游流式连接刚刚中断") {
		t.Fatalf("recovery metadata leaked into the conversation: %s", output)
	}
}

func TestForwardAnthropicDoesNotReplayAfterPartialStreamDisconnect(t *testing.T) {
	previous := currentStreamRecoveryPolicy()
	ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: true, MaxAttempts: 2, MaxDelaySeconds: 1})
	defer ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: previous.enabled, MaxAttempts: previous.maxAttempts, MaxDelaySeconds: previous.maxDelaySeconds})

	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-partial\",\"model\":\"claude-test\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"第一段\"}}\n\n")
		// End before message_stop to simulate a dropped upstream stream.
	}))
	defer upstream.Close()

	model := &storage.CustomModel{Provider: "anthropic", APIStyle: "messages", APIURL: upstream.URL, APIKey: "test-key", ExternalModelName: "claude-test"}
	recorder := httptest.NewRecorder()
	forwardAnthropic(recorder, httptest.NewRequest(http.MethodPost, "/v1internal:streamGenerateContent", nil), model, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "请回答"}}}},
	}, "claude-partial-stream")

	if requests != 1 {
		t.Fatalf("Claude partial stream was replayed %d times", requests)
	}
	output := recorder.Body.String()
	if recorder.Code != http.StatusOK || strings.Count(output, "第一段") != 1 || !strings.Contains(output, `"finishReason":"STOP"`) {
		t.Fatalf("Claude partial stream did not finish safely: %d %s", recorder.Code, output)
	}
}

func TestStreamRecoveryOnlyRetriesExplicitRejectionBeforeCommit(t *testing.T) {
	policy := streamRecoveryPolicy{enabled: true, maxAttempts: 2, maxDelaySeconds: 1}
	writer := newDownstreamSSEWriter(httptest.NewRecorder())
	if !canRetryRejectedRequest(writer, policy, 1) {
		t.Fatal("an explicit rejection before downstream output should be eligible for one safe retry")
	}
	writer.committed = true
	if canRetryRejectedRequest(writer, policy, 1) {
		t.Fatal("a committed stream must never be replayed")
	}
	writer.committed = false
	if canRetryRejectedRequest(writer, policy, 3) {
		t.Fatal("retry budget must be enforced")
	}
	policy.enabled = false
	if canRetryRejectedRequest(writer, policy, 1) {
		t.Fatal("disabled recovery must not retry an upstream rejection")
	}
}

func TestForwardOpenAIChatDoesNotReplayAfterRoleOnlyStream(t *testing.T) {
	previous := currentStreamRecoveryPolicy()
	ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: true, MaxAttempts: 2, MaxDelaySeconds: 1})
	defer ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: previous.enabled, MaxAttempts: previous.maxAttempts, MaxDelaySeconds: previous.maxDelaySeconds})

	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"first\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n")
		// A role/reasoning event proves that the upstream accepted the request,
		// but it has no user-visible text to convert.
	}))
	defer upstream.Close()

	model := &storage.CustomModel{Provider: "openai", APIURL: upstream.URL + "/v1", APIKey: "test-key", ExternalModelName: "gpt-test"}
	recorder := httptest.NewRecorder()
	forwardOpenAIChat(recorder, httptest.NewRequest(http.MethodPost, "/v1internal:streamGenerateContent", nil), model, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "请回答"}}}},
	}, "role-only")

	if requests != 1 {
		t.Fatalf("upstream requests = %d, want 1: an accepted stream must never be replayed", requests)
	}
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for an incomplete uncommitted stream: %s", recorder.Code, recorder.Body.String())
	}
}

func TestBoundAccountPoolRetriesPinnedAccountAfterTransientQuotaResponse(t *testing.T) {
	storage.Init(t.TempDir())

	var firstCalls, secondCalls atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := firstCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer first-token" {
			t.Errorf("first account authorization = %q", got)
		}
		if call == 1 {
			w.Header().Set("X-RateLimit-Remaining-Requests", "0")
			w.Header().Set("X-RateLimit-Reset-Requests", "30s")
			w.Header().Set("Retry-After", "0")
			http.Error(w, `{"error":{"message":"quota exhausted"}}`, http.StatusTooManyRequests)
			return
		}
		w.Header().Set("X-RateLimit-Remaining-Requests", "9")
		w.Header().Set("X-RateLimit-Remaining-Tokens", "1000")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-2\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer second-token" {
			t.Errorf("second account authorization = %q", got)
		}
		w.Header().Set("X-RateLimit-Remaining-Requests", "9")
		w.Header().Set("X-RateLimit-Remaining-Tokens", "1000")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-2\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"))
	}))
	defer second.Close()

	for _, account := range []storage.UpstreamAccount{
		{ID: "first", Name: "first", Provider: "openai", Type: "api_key", APIURL: first.URL, APIKey: "first-token", AuthMode: "bearer", Enabled: true, Priority: 1, MaxConcurrency: 1},
		{ID: "second", Name: "second", Provider: "openai", Type: "api_key", APIURL: second.URL, APIKey: "second-token", AuthMode: "bearer", Enabled: true, Priority: 2, MaxConcurrency: 1},
	} {
		if err := storage.SaveUpstreamAccount(account); err != nil {
			t.Fatal(err)
		}
	}

	previous := currentStreamRecoveryPolicy()
	ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: true, MaxAttempts: 2, MaxDelaySeconds: 1})
	defer ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: previous.enabled, MaxAttempts: previous.maxAttempts, MaxDelaySeconds: previous.maxDelaySeconds})

	model := &storage.CustomModel{
		Name: "models/gpt-test", DisplayName: "gpt-test", Provider: "openai", ExternalModelName: "gpt-test",
		AccountIDs: []string{"first", "second"}, APIStyle: "chat_completions",
	}
	recorder := httptest.NewRecorder()
	incoming := httptest.NewRequest(http.MethodPost, "/v1internal:generateContent", nil)
	forwardOpenAIChat(recorder, incoming, model, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}}},
	}, "pool-failover")

	if firstCalls.Load() != 2 || secondCalls.Load() != 0 {
		t.Fatalf("account calls = first:%d second:%d, want two attempts on the pinned first account", firstCalls.Load(), secondCalls.Load())
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "ok") {
		t.Fatalf("unexpected downstream response: %d %s", recorder.Code, recorder.Body.String())
	}
	accounts, err := storage.LoadUpstreamAccounts()
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]storage.UpstreamAccount{}
	for _, account := range accounts {
		status[account.ID] = account
	}
	if status["first"].CooldownUntil != "" || status["first"].FailureCount != 0 || status["first"].LastSuccessAt == "" {
		t.Fatalf("recovered account retained a cross-request blacklist: %#v", status["first"])
	}
	if quota := status["first"].Quota; !quota.Available || quota.StatusCode != http.StatusOK || quota.RequestsRemaining != "9" || quota.TokensRemaining != "1000" {
		t.Fatalf("recovered account quota was not updated from the successful retry: %#v", quota)
	}
	if status["second"].LastSuccessAt != "" || status["second"].ActiveRequests != 0 {
		t.Fatalf("second account was unexpectedly used: %#v", status["second"])
	}
}

func TestConnectionFailureBeforeRequestWriteRetriesAndCompletes(t *testing.T) {
	previousPolicy := currentStreamRecoveryPolicy()
	ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: true, MaxAttempts: 1, MaxDelaySeconds: 1})
	defer ConfigureStreamRecovery(storage.StreamRecoverySettings{
		Enabled: previousPolicy.enabled, MaxAttempts: previousPolicy.maxAttempts, MaxDelaySeconds: previousPolicy.maxDelaySeconds,
	})

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-safe-retry\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstreamServer.Close()

	originalTransport := http.DefaultTransport
	var transportCalls atomic.Int32
	http.DefaultTransport = modelFetchRoundTripper(func(request *http.Request) (*http.Response, error) {
		if transportCalls.Add(1) == 1 {
			// Returning before delegating means net/http never invokes
			// WroteRequest; the proxy can prove no upstream request was sent.
			return nil, fmt.Errorf("temporary dial failure before request write")
		}
		return originalTransport.RoundTrip(request)
	})
	defer func() { http.DefaultTransport = originalTransport }()

	model := &storage.CustomModel{
		Provider: "openai", APIStyle: "chat_completions", APIURL: upstreamServer.URL,
		APIKey: "test-key", ExternalModelName: "gpt-test",
	}
	recorder := httptest.NewRecorder()
	forwardOpenAIChat(recorder, httptest.NewRequest(http.MethodPost, "/v1internal:streamGenerateContent", nil), model, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}}},
	}, "safe-transport-retry")

	if transportCalls.Load() != 2 {
		t.Fatalf("transport attempts = %d, want one safe retry", transportCalls.Load())
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"text":"ok"`) {
		t.Fatalf("safe connection retry did not complete normally: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestTransientProvider403RetriesSameDirectUpstreamThenCompletes(t *testing.T) {
	previous := currentStreamRecoveryPolicy()
	ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: true, MaxAttempts: 2, MaxDelaySeconds: 1})
	defer ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: previous.enabled, MaxAttempts: previous.maxAttempts, MaxDelaySeconds: previous.maxDelaySeconds})

	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"permission_error","message":"The request is prohibited due to a violation of provider Terms Of Service.","error_type":"permission_denied"},"metadata":{"provider_name":null}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-ok\",\"model\":\"claude-test\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"恢复成功\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()

	model := &storage.CustomModel{Provider: "anthropic", APIStyle: "messages", APIURL: upstream.URL, APIKey: "test-key", ExternalModelName: "claude-test"}
	recorder := httptest.NewRecorder()
	forwardAnthropic(recorder, httptest.NewRequest(http.MethodPost, "/v1internal:streamGenerateContent", nil), model, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}}},
	}, "provider-403-recovery")

	if calls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want one safe retry", calls.Load())
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "恢复成功") || !strings.Contains(recorder.Body.String(), `"finishReason":"STOP"`) {
		t.Fatalf("unexpected recovered response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestPersistentTransientProvider403EndsTurnWithoutAgentError(t *testing.T) {
	previous := currentStreamRecoveryPolicy()
	ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: true, MaxAttempts: 2, MaxDelaySeconds: 1})
	defer ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: previous.enabled, MaxAttempts: previous.maxAttempts, MaxDelaySeconds: previous.maxDelaySeconds})

	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"permission_error","message":"The request is prohibited due to a violation of provider Terms Of Service.","error_type":"permission_denied"},"metadata":{"provider_name":null}}`)
	}))
	defer upstream.Close()

	model := &storage.CustomModel{Provider: "anthropic", APIStyle: "messages", APIURL: upstream.URL, APIKey: "test-key", ExternalModelName: "claude-test"}
	recorder := httptest.NewRecorder()
	forwardAnthropic(recorder, httptest.NewRequest(http.MethodPost, "/v1internal:streamGenerateContent", nil), model, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}}},
	}, "provider-403-stop")

	if calls.Load() != 3 {
		t.Fatalf("upstream calls = %d, want initial request plus two bounded retries", calls.Load())
	}
	output := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(output, "第三方上游暂时没有可用线路") || !strings.Contains(output, `"finishReason":"STOP"`) {
		t.Fatalf("temporary failure did not become a completed Antigravity turn: %d %s", recorder.Code, output)
	}
}

func TestTransientGatewayModelPool404WaitsForRefillThenCompletes(t *testing.T) {
	previous := currentStreamRecoveryPolicy()
	ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: true, MaxAttempts: 2, MaxDelaySeconds: 1})
	defer ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: previous.enabled, MaxAttempts: previous.maxAttempts, MaxDelaySeconds: previous.maxDelaySeconds})

	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"message":"Model gpt-test is not supported by any configured account in this group","type":"model_not_found"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-refilled\",\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"补号后恢复\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()

	model := &storage.CustomModel{Provider: "openai", APIStyle: "chat_completions", APIURL: upstream.URL, APIKey: "test-key", ExternalModelName: "gpt-test"}
	recorder := httptest.NewRecorder()
	forwardOpenAIChat(recorder, httptest.NewRequest(http.MethodPost, "/v1internal:streamGenerateContent", nil), model, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}}},
	}, "model-pool-refill")

	if calls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want one safe retry after pool refill", calls.Load())
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "补号后恢复") || !strings.Contains(recorder.Body.String(), `"finishReason":"STOP"`) {
		t.Fatalf("unexpected recovered response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRejectedRetryAfterUsesBoundedProviderRefillDelays(t *testing.T) {
	provider403 := `{"error":{"type":"permission_error","message":"provider Terms Of Service","error_type":"permission_denied"}}`
	pool404 := `{"error":{"message":"not supported by any configured account in this group","type":"model_not_found"}}`
	permanent403 := `{"error":{"message":"insufficient balance","type":"billing_error"}}`
	cases := []struct {
		name                     string
		status, reconnect        int
		body, upstream, expected string
	}{
		{name: "provider first", status: http.StatusForbidden, reconnect: 1, body: provider403, expected: "1.500"},
		{name: "provider second", status: http.StatusForbidden, reconnect: 2, body: provider403, expected: "3.000"},
		{name: "empty model pool", status: http.StatusNotFound, reconnect: 1, body: pool404, expected: "1.500"},
		{name: "rate limit", status: http.StatusTooManyRequests, reconnect: 1, expected: "2.000"},
		{name: "gateway second", status: http.StatusBadGateway, reconnect: 2, expected: "2.000"},
		{name: "explicit header", status: http.StatusForbidden, reconnect: 1, body: provider403, upstream: "7", expected: "7"},
		{name: "permanent balance", status: http.StatusForbidden, reconnect: 1, body: permanent403, expected: ""},
		{name: "unknown model", status: http.StatusNotFound, reconnect: 1, body: `{"error":{"message":"model not found"}}`, expected: ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := rejectedRetryAfter(test.status, test.body, test.upstream, test.reconnect); got != test.expected {
				t.Fatalf("rejectedRetryAfter() = %q, want %q", got, test.expected)
			}
		})
	}
}

func TestPermanentUpstreamRejectionsEndTurnWithoutAutomaticReplay(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{name: "insufficient balance", statusCode: http.StatusForbidden, body: `{"error":{"message":"insufficient balance","type":"billing_error"}}`, want: "余额、额度或权限不足"},
		{name: "model route missing", statusCode: http.StatusNotFound, body: `{"error":{"message":"Model \\"gpt-test\\" not found","type":"model_not_found"}}`, want: "没有为当前账户配置所选模型"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(test.statusCode)
				_, _ = io.WriteString(w, test.body)
			}))
			defer upstream.Close()

			model := &storage.CustomModel{Provider: "openai", APIStyle: "chat_completions", APIURL: upstream.URL, APIKey: "test-key", ExternalModelName: "gpt-test"}
			recorder := httptest.NewRecorder()
			forwardOpenAIChat(recorder, httptest.NewRequest(http.MethodPost, "/v1internal:streamGenerateContent", nil), model, map[string]any{
				"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}}},
			}, "permanent-rejection")

			if calls.Load() != 1 {
				t.Fatalf("upstream calls = %d, permanent rejection must not be replayed", calls.Load())
			}
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), test.want) || !strings.Contains(recorder.Body.String(), `"finishReason":"STOP"`) {
				t.Fatalf("permanent rejection did not produce one actionable completed turn: %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestBoundAccountPermanentFailureDoesNotSwitchAccountsOrReturnHTTP503(t *testing.T) {
	storage.Init(t.TempDir())
	previous := currentStreamRecoveryPolicy()
	ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: true, MaxAttempts: 2, MaxDelaySeconds: 1})
	defer ConfigureStreamRecovery(storage.StreamRecoverySettings{Enabled: previous.enabled, MaxAttempts: previous.maxAttempts, MaxDelaySeconds: previous.maxDelaySeconds})

	var firstCalls, secondCalls atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"message":"insufficient balance","type":"billing_error"}}`)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"message":"insufficient balance","type":"billing_error"}}`)
	}))
	defer second.Close()

	for _, account := range []storage.UpstreamAccount{
		{ID: "first-exhausted", Name: "first", Provider: "openai", Type: "api_key", APIURL: first.URL, APIKey: "first-token", AuthMode: "bearer", Enabled: true, Priority: 1, MaxConcurrency: 1},
		{ID: "second-exhausted", Name: "second", Provider: "openai", Type: "api_key", APIURL: second.URL, APIKey: "second-token", AuthMode: "bearer", Enabled: true, Priority: 2, MaxConcurrency: 1},
	} {
		if err := storage.SaveUpstreamAccount(account); err != nil {
			t.Fatal(err)
		}
	}

	model := &storage.CustomModel{Provider: "openai", APIStyle: "chat_completions", ExternalModelName: "gpt-test", AccountIDs: []string{"first-exhausted", "second-exhausted"}}
	recorder := httptest.NewRecorder()
	forwardOpenAIChat(recorder, httptest.NewRequest(http.MethodPost, "/v1internal:streamGenerateContent", nil), model, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}}},
	}, "pool-exhausted")

	if firstCalls.Load() != 1 || secondCalls.Load() != 0 {
		t.Fatalf("pool calls = first:%d second:%d, permanent failure must stay on the selected account", firstCalls.Load(), secondCalls.Load())
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "余额、额度或权限不足") || !strings.Contains(recorder.Body.String(), `"finishReason":"STOP"`) {
		t.Fatalf("exhausted pool terminated Antigravity instead of completing the turn: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCleanPatchedPath(t *testing.T) {
	cases := map[string]string{
		"/v1internal/antigravity-wf/v1internal:streamGenerateContent":   "/v1internal:streamGenerateContent",
		"/v1internal/antigravity-wf/v1internal/cascadeNuxes":            "/v1internal/cascadeNuxes",
		"/v1internal/wfproxy/v1internal:generateContent":                "/v1internal:generateContent",
		"/v1internal/wfproxy/v1internal/cascadeNuxes":                   "/v1internal/cascadeNuxes",
		"/v1internal/wfproxy-sandbox/v1internal:fetchAvailableModels":   "/v1internal:fetchAvailableModels",
		"/v1internal/wfproxy-sandbox/v1internal/cascadeNuxes":           "/v1internal/cascadeNuxes",
		"/v1internal/antigravity-byok/v1internal:streamGenerateContent": "/v1internal:streamGenerateContent",
		"/v1internal/antigravity-byok/v1internal/cascadeNuxes":          "/v1internal/cascadeNuxes",
		"/v1internal/byokxxx/v1internal:generateContent":                "/v1internal:generateContent",
		"/v1internal/byokxxx/v1internal/cascadeNuxes":                   "/v1internal/cascadeNuxes",
		"/v1internal/byokxxx-sandbox/v1internal:fetchAvailableModels":   "/v1internal:fetchAvailableModels",
		"/v1internal/byokxxx-sandbox/v1internal/cascadeNuxes":           "/v1internal/cascadeNuxes",
		"/v1internal/xxxxxxxxxxxx/v1internal:fetchAvailableModels":      "/v1internal:fetchAvailableModels",
		"/v1internal:retrieveUserQuota":                                 "/v1internal:retrieveUserQuota",
	}
	for input, want := range cases {
		if got := cleanPatchedPath(input); got != want {
			t.Errorf("cleanPatchedPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHealthEndpointsExposeCanonicalIdentityAndLegacyUpgradeCompatibility(t *testing.T) {
	canonical := httptest.NewRecorder()
	handleRequest(canonical, httptest.NewRequest(http.MethodGet, "/_antigravity-wf/health", nil))
	if canonical.Code != http.StatusOK || canonical.Header().Get("X-Antigravity-WF") != "go-proxy" {
		t.Fatalf("canonical health response = status:%d headers:%v", canonical.Code, canonical.Header())
	}
	if !strings.Contains(canonical.Body.String(), `"proxy":"antigravity-wf"`) {
		t.Fatalf("canonical health body = %s", canonical.Body.String())
	}

	legacy := httptest.NewRecorder()
	handleRequest(legacy, httptest.NewRequest(http.MethodGet, legacyProxyHealthPath(), nil))
	if legacy.Code != http.StatusOK || legacy.Header().Get("X-Antigravity-BYOK") != "go-proxy" {
		t.Fatalf("legacy upgrade health response = status:%d headers:%v", legacy.Code, legacy.Header())
	}
}
