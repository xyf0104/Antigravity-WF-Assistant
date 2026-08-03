package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"testing"

	"antigravity-byok/internal/storage"
	"github.com/andybalholm/brotli"
)

func TestInjectCustomModelsKeepsWrappedNativeShapeAndPickerIndexes(t *testing.T) {
	storage.Init(t.TempDir())
	model := storage.NewDiscoveredModel("openai", "https://api.example.test", "secret", "gpt-custom")
	parsed := map[string]any{
		"response": map[string]any{
			"models": []any{map[string]any{
				"name": "models/native", "model": "MODEL_PLACEHOLDER_M0", "displayName": "Native",
			}},
			"agentModelSorts": []any{map[string]any{
				"groups": []any{map[string]any{"modelIds": []any{"native-model"}}},
			}},
		},
	}

	summary := injectCustomModels(parsed, []storage.CustomModel{model})
	if err := validateModelInjection(parsed, []storage.CustomModel{model}, summary); err != nil {
		t.Fatal(err)
	}
	if summary.customCount != 1 || len(summary.indexPaths) == 0 {
		t.Fatalf("unexpected injection summary: %+v", summary)
	}
	root := parsed["response"].(map[string]any)
	entries := root["models"].([]any)
	found := false
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		if entry["name"] == "models/"+getModelSlug(model) {
			found = true
			if entry["displayName"] != "gpt-custom" || entry["supportsImages"] != true || entry["supportsAudio"] != false || entry["supportsVideo"] != false || entry["supportsWebSearch"] != true {
				t.Fatalf("injected capability/name fields are incomplete: %#v", entry)
			}
		}
	}
	if !found {
		t.Fatalf("custom model was not injected into wrapped array: %#v", entries)
	}
	if !responseIndexesModel(collectModelResponseRoots(parsed), getModelSlug(model), getModelPlaceholder(model)) {
		t.Fatal("custom model was not added to a picker index")
	}
}

func TestDecodeModelResponseEncodings(t *testing.T) {
	plain := []byte(`{"models":{"native":{"model":"MODEL_PLACEHOLDER_M1"}}}`)
	cases := []struct {
		name     string
		encoding string
		encode   func([]byte) []byte
	}{
		{name: "identity", encoding: "identity", encode: func(value []byte) []byte { return value }},
		{name: "gzip", encoding: "gzip", encode: func(value []byte) []byte {
			var out bytes.Buffer
			writer := gzip.NewWriter(&out)
			_, _ = writer.Write(value)
			_ = writer.Close()
			return out.Bytes()
		}},
		{name: "zlib", encoding: "deflate", encode: func(value []byte) []byte {
			var out bytes.Buffer
			writer := zlib.NewWriter(&out)
			_, _ = writer.Write(value)
			_ = writer.Close()
			return out.Bytes()
		}},
		{name: "raw-deflate", encoding: "deflate", encode: func(value []byte) []byte {
			var out bytes.Buffer
			writer, _ := flate.NewWriter(&out, flate.DefaultCompression)
			_, _ = writer.Write(value)
			_ = writer.Close()
			return out.Bytes()
		}},
		{name: "brotli", encoding: "br", encode: func(value []byte) []byte {
			var out bytes.Buffer
			writer := brotli.NewWriter(&out)
			_, _ = writer.Write(value)
			_ = writer.Close()
			return out.Bytes()
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decoded, err := decodeModelResponse(testCase.encode(plain), testCase.encoding)
			if err != nil {
				t.Fatal(err)
			}
			var parsed map[string]any
			if err := json.Unmarshal(decoded, &parsed); err != nil {
				t.Fatal(err)
			}
			if parsed["models"] == nil {
				t.Fatalf("decoded response lost models: %s", decoded)
			}
		})
	}
}
