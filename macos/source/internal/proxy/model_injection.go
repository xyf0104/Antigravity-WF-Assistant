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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"antigravity-byok/internal/storage"
	"github.com/andybalholm/brotli"
)

var modelContainerKeys = []string{"models", "availableModels", "available_models"}
var modelWrapperKeys = []string{"response", "result", "data"}
var modelSortKeys = []string{"agentModelSorts", "battleModeModelSorts"}
var modelIDIndexKeys = map[string]bool{
	"tieredModelIds":      true,
	"availableModelIds":   true,
	"allowedModelIds":     true,
	"allowlistedModelIds": true,
}

type modelResponseRoot struct {
	path  string
	value map[string]any
}

type modelInjectionSummary struct {
	officialCount  int
	customCount    int
	customNames    []string
	customSlugs    []string
	containers     []string
	indexPaths     []string
	officialSample any
	customSample   any
}

func allocateModelSlugs(models []storage.CustomModel, usedModelIDs map[string]struct{}) map[string]string {
	used := make(map[string]struct{}, len(usedModelIDs)+len(models))
	for id := range usedModelIDs {
		id = strings.TrimPrefix(id, "models/")
		if id != "" {
			used[id] = struct{}{}
		}
	}
	ordered := append([]storage.CustomModel(nil), models...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return modelPlaceholderKey(ordered[i]) < modelPlaceholderKey(ordered[j])
	})
	assignments := make(map[string]string, len(ordered))
	for _, model := range ordered {
		base := baseModelSlug(model)
		candidate := base
		for suffix := 2; ; suffix++ {
			if _, exists := used[candidate]; !exists {
				break
			}
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		assignments[modelPlaceholderKey(model)] = candidate
		used[candidate] = struct{}{}
	}
	slugMu.Lock()
	allocatedSlugs = assignments
	slugMu.Unlock()
	return assignments
}

func modelDisplayName(m storage.CustomModel) string {
	if strings.TrimSpace(m.DisplayName) != "" {
		return m.DisplayName
	}
	if strings.TrimSpace(m.ExternalModelName) != "" {
		return m.ExternalModelName
	}
	return strings.TrimPrefix(m.Name, "models/")
}

func collectModelResponseRoots(parsed map[string]any) []modelResponseRoot {
	roots := make([]modelResponseRoot, 0, 4)
	var visit func(map[string]any, string, int)
	visit = func(value map[string]any, path string, depth int) {
		roots = append(roots, modelResponseRoot{path: path, value: value})
		if depth >= 6 {
			return
		}
		for _, key := range modelWrapperKeys {
			child, ok := value[key].(map[string]any)
			if !ok {
				continue
			}
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			visit(child, childPath, depth+1)
		}
	}
	visit(parsed, "", 0)
	return roots
}

func modelPath(rootPath, key string) string {
	if rootPath == "" {
		return key
	}
	return rootPath + "." + key
}

func collectOfficialModelEntries(roots []modelResponseRoot) map[string]any {
	official := make(map[string]any)
	for _, root := range roots {
		for _, key := range modelContainerKeys {
			switch container := root.value[key].(type) {
			case map[string]any:
				for id, entry := range container {
					official[modelPath(root.path, key)+":"+id] = entry
				}
			case []any:
				for index, entry := range container {
					official[fmt.Sprintf("%s:%d", modelPath(root.path, key), index)] = entry
				}
			}
		}
	}
	return official
}

func collectUsedModelIDs(roots []modelResponseRoot) map[string]struct{} {
	used := make(map[string]struct{})
	for _, root := range roots {
		for _, key := range modelContainerKeys {
			switch container := root.value[key].(type) {
			case map[string]any:
				for id := range container {
					used[id] = struct{}{}
				}
			case []any:
				for _, raw := range container {
					entry, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					for _, field := range []string{"name", "id", "modelId", "model_id"} {
						if value, ok := entry[field].(string); ok && value != "" {
							used[value] = struct{}{}
						}
					}
				}
			}
		}
	}
	return used
}

func buildArrayModelEntry(m storage.CustomModel, slug, placeholder string) map[string]any {
	entry := buildFakeModelEntry(m, placeholder)
	entry["name"] = "models/" + slug
	entry["version"] = "1.0"
	entry["inputTokenLimit"] = 1048576
	entry["outputTokenLimit"] = 65536
	entry["supportedGenerationMethods"] = []any{"generateContent", "streamGenerateContent", "countTokens"}
	return entry
}

func arrayHasInjectedModel(entries []any, slug, placeholder string) bool {
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if entry["name"] == "models/"+slug || entry["name"] == slug || entry["model"] == placeholder {
			return true
		}
	}
	return false
}

// injectCustomModels supports root and wrapped model responses, map and array
// containers, and every currently known picker index.
func injectCustomModels(parsed map[string]any, models []storage.CustomModel) modelInjectionSummary {
	summary := modelInjectionSummary{}
	roots := collectModelResponseRoots(parsed)
	official := collectOfficialModelEntries(roots)
	slugAssignments := allocateModelSlugs(models, collectUsedModelIDs(roots))
	assignments := allocateModelPlaceholders(models, official)
	slugs := make([]string, 0, len(models))
	for _, model := range models {
		key := modelPlaceholderKey(model)
		if assignments[key] == "" || slugAssignments[key] == "" {
			continue
		}
		slugs = append(slugs, slugAssignments[key])
		summary.customNames = append(summary.customNames, modelDisplayName(model))
		summary.customSlugs = append(summary.customSlugs, slugAssignments[key])
	}
	summary.customCount = len(slugs)

	var indexedRoots []modelResponseRoot
	for _, root := range roots {
		rootInjected := false
		for _, key := range modelContainerKeys {
			raw, exists := root.value[key]
			if !exists {
				continue
			}
			switch container := raw.(type) {
			case map[string]any:
				if len(container) > summary.officialCount {
					summary.officialCount = len(container)
				}
				for _, entry := range container {
					if summary.officialSample == nil {
						summary.officialSample = entry
					}
					break
				}
				for _, model := range models {
					key := modelPlaceholderKey(model)
					placeholder, slug := assignments[key], slugAssignments[key]
					if placeholder == "" || slug == "" {
						continue
					}
					entry := buildFakeModelEntry(model, placeholder)
					container[slug] = entry
					if summary.customSample == nil {
						summary.customSample = entry
					}
				}
				summary.containers = append(summary.containers, modelPath(root.path, key)+":map")
				rootInjected = true
			case []any:
				if len(container) > summary.officialCount {
					summary.officialCount = len(container)
				}
				if len(container) > 0 && summary.officialSample == nil {
					summary.officialSample = container[0]
				}
				injected := make([]any, 0, len(models))
				for _, model := range models {
					key := modelPlaceholderKey(model)
					placeholder, slug := assignments[key], slugAssignments[key]
					if placeholder == "" || slug == "" || arrayHasInjectedModel(container, slug, placeholder) {
						continue
					}
					entry := buildArrayModelEntry(model, slug, placeholder)
					injected = append(injected, entry)
					if summary.customSample == nil {
						summary.customSample = entry
					}
				}
				root.value[key] = append(injected, container...)
				summary.containers = append(summary.containers, modelPath(root.path, key)+":array")
				rootInjected = true
			}
		}
		if rootInjected {
			indexedRoots = append(indexedRoots, root)
		}
	}
	if len(summary.containers) == 0 && len(models) > 0 {
		target := roots[len(roots)-1]
		container := make(map[string]any, len(models))
		for _, model := range models {
			key := modelPlaceholderKey(model)
			placeholder, slug := assignments[key], slugAssignments[key]
			if placeholder == "" || slug == "" {
				continue
			}
			entry := buildFakeModelEntry(model, placeholder)
			container[slug] = entry
			if summary.customSample == nil {
				summary.customSample = entry
			}
		}
		target.value["models"] = container
		summary.containers = append(summary.containers, modelPath(target.path, "models")+":created-map")
		indexedRoots = append(indexedRoots, target)
	}
	for _, root := range indexedRoots {
		summary.indexPaths = append(summary.indexPaths, addModelIndexes(root.value, root.path, slugs)...)
	}
	summary.indexPaths = uniqueStrings(summary.indexPaths)
	return summary
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func addModelIndexes(parsed map[string]any, rootPath string, modelIDs []string) []string {
	if len(modelIDs) == 0 {
		return nil
	}
	var paths []string
	agentPresent := false
	for _, key := range modelSortKeys {
		_, exists := parsed[key]
		if key == "agentModelSorts" {
			agentPresent = exists
		}
		if !exists {
			continue
		}
		parsed[key] = addModelSortIDs(parsed[key], modelIDs)
		paths = append(paths, modelPath(rootPath, key)+".groups[].modelIds")
	}
	if !agentPresent {
		parsed["agentModelSorts"] = addModelSortIDs(nil, modelIDs)
		paths = append(paths, modelPath(rootPath, "agentModelSorts")+".groups[].modelIds")
	}
	for key := range modelIDIndexKeys {
		value, exists := parsed[key]
		if !exists {
			continue
		}
		if updated, changed := prependModelIDs(value, modelIDs); changed {
			parsed[key] = updated
			paths = append(paths, modelPath(rootPath, key))
		}
	}
	return paths
}

func addModelSortIDs(raw any, modelIDs []string) []any {
	sorts, _ := raw.([]any)
	inserted := false
	for sortIndex, rawSort := range sorts {
		sortEntry, ok := rawSort.(map[string]any)
		if !ok {
			continue
		}
		groups, _ := sortEntry["groups"].([]any)
		for groupIndex, rawGroup := range groups {
			group, ok := rawGroup.(map[string]any)
			if !ok {
				continue
			}
			ids, _ := group["modelIds"].([]any)
			group["modelIds"] = prependUniqueModelIDs(ids, modelIDs)
			groups[groupIndex] = group
			inserted = true
		}
		sortEntry["groups"] = groups
		sorts[sortIndex] = sortEntry
	}
	if !inserted {
		ids := make([]any, len(modelIDs))
		for index, modelID := range modelIDs {
			ids[index] = modelID
		}
		sorts = append([]any{map[string]any{
			"displayName": "Custom",
			"groups":      []any{map[string]any{"modelIds": ids}},
		}}, sorts...)
	}
	return sorts
}

func prependUniqueModelIDs(existing []any, modelIDs []string) []any {
	custom := make([]any, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		found := false
		for _, value := range existing {
			if value == modelID || value == "models/"+modelID {
				found = true
				break
			}
		}
		if !found {
			custom = append(custom, modelID)
		}
	}
	return append(custom, existing...)
}

func prependModelIDs(value any, modelIDs []string) (any, bool) {
	switch current := value.(type) {
	case []any:
		isStringList := len(current) == 0
		for _, item := range current {
			if _, ok := item.(string); ok {
				isStringList = true
				continue
			}
			isStringList = false
			break
		}
		if isStringList {
			return prependUniqueModelIDs(current, modelIDs), true
		}
		changed := false
		for index, item := range current {
			if updated, itemChanged := prependModelIDs(item, modelIDs); itemChanged {
				current[index] = updated
				changed = true
			}
		}
		return current, changed
	case map[string]any:
		changed := false
		for key, item := range current {
			if updated, itemChanged := prependModelIDs(item, modelIDs); itemChanged {
				current[key] = updated
				changed = true
			}
		}
		return current, changed
	default:
		return value, false
	}
}

func validateModelInjection(parsed map[string]any, models []storage.CustomModel, summary modelInjectionSummary) error {
	if len(models) == 0 {
		return nil
	}
	if summary.customCount != len(models) {
		return fmt.Errorf("只为 %d/%d 个自定义模型分配了有效标识", summary.customCount, len(models))
	}
	roots := collectModelResponseRoots(parsed)
	for _, model := range models {
		slug := getModelSlug(model)
		placeholder := getModelPlaceholder(model)
		if !responseContainsModel(roots, slug, placeholder) {
			return fmt.Errorf("注入后未在模型容器中找到 %s", slug)
		}
		if !responseIndexesModel(roots, slug, placeholder) {
			return fmt.Errorf("注入后没有模型选择索引引用 %s", slug)
		}
	}
	return nil
}

func responseContainsModel(roots []modelResponseRoot, slug, placeholder string) bool {
	for _, root := range roots {
		for _, key := range modelContainerKeys {
			switch container := root.value[key].(type) {
			case map[string]any:
				if _, ok := container[slug]; ok {
					return true
				}
			case []any:
				if arrayHasInjectedModel(container, slug, placeholder) {
					return true
				}
			}
		}
	}
	return false
}

func responseIndexesModel(roots []modelResponseRoot, slug, placeholder string) bool {
	wanted := map[string]bool{slug: true, "models/" + slug: true, placeholder: true, "models/" + placeholder: true}
	for _, root := range roots {
		for key, value := range root.value {
			lower := strings.ToLower(key)
			if strings.HasSuffix(lower, "modelids") || strings.HasSuffix(lower, "model_ids") || key == "agentModelSorts" || key == "battleModeModelSorts" {
				if containsModelReference(value, wanted) {
					return true
				}
			}
		}
	}
	return false
}

func containsModelReference(value any, wanted map[string]bool) bool {
	switch current := value.(type) {
	case string:
		return wanted[current]
	case []any:
		for _, item := range current {
			if containsModelReference(item, wanted) {
				return true
			}
		}
	case map[string]any:
		for _, item := range current {
			if containsModelReference(item, wanted) {
				return true
			}
		}
	}
	return false
}

// handleFetchAvailableModels forwards Google's model response and injects the
// configured models into current and legacy Antigravity response shapes.
func handleFetchAvailableModels(w http.ResponseWriter, r *http.Request) {
	handleFetchAvailableModelsWithClient(w, r, &http.Client{Timeout: 30 * time.Second})
}

// handleFetchAvailableModelsWithClient isolates the upstream transport so the
// model-response conversion can be verified locally without contacting Google.
// Production always calls the wrapper above with a bounded HTTP client.
func handleFetchAvailableModelsWithClient(w http.ResponseWriter, r *http.Request, client *http.Client) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, googleBaseURL+"/v1internal:fetchAvailableModels", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("Host", googleHost)
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil {
		trace("model-response-error", map[string]any{"message": err.Error()})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		trace("model-response-error", map[string]any{"statusCode": resp.StatusCode, "encoding": resp.Header.Get("Content-Encoding"), "message": err.Error()})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	encoding := resp.Header.Get("Content-Encoding")
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		trace("model-response-error", map[string]any{"statusCode": resp.StatusCode, "encoding": encoding, "message": fmt.Sprintf("模型上游返回 HTTP %d", resp.StatusCode)})
		forwardRawModelResponse(w, resp, body)
		return
	}
	decoded, err := decodeModelResponse(body, encoding)
	if err != nil {
		trace("model-response-error", map[string]any{"statusCode": resp.StatusCode, "encoding": encoding, "message": err.Error()})
		forwardRawModelResponse(w, resp, body)
		return
	}
	var raw any
	if err := json.Unmarshal(decoded, &raw); err != nil {
		trace("model-response-error", map[string]any{"statusCode": resp.StatusCode, "encoding": encoding, "message": fmt.Sprintf("模型响应 JSON 解析失败: %v", err)})
		forwardRawModelResponse(w, resp, body)
		return
	}
	parsed, ok := raw.(map[string]any)
	if !ok {
		trace("model-response-error", map[string]any{"statusCode": resp.StatusCode, "encoding": encoding, "message": "模型响应 JSON 根节点不是对象"})
		forwardRawModelResponse(w, resp, body)
		return
	}
	models, loadErr := storage.LoadModels()
	if loadErr != nil {
		trace("model-injection-error", map[string]any{"message": fmt.Sprintf("读取自定义模型失败: %v", loadErr)})
		models = nil
	}
	summary := injectCustomModels(parsed, models)
	validationErr := validateModelInjection(parsed, models, summary)
	if validationErr != nil {
		trace("model-injection-error", map[string]any{"configuredCount": len(models), "customCount": summary.customCount, "containers": summary.containers, "indexPaths": summary.indexPaths, "message": validationErr.Error()})
	}
	if err := saveModelStructureSnapshot(parsed, summary, resp.StatusCode, encoding, validationErr); err != nil {
		trace("model-snapshot-error", map[string]any{"message": err.Error()})
	}
	out, err := json.Marshal(parsed)
	if err != nil {
		trace("model-response-error", map[string]any{"statusCode": resp.StatusCode, "encoding": encoding, "message": err.Error()})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	copyDecodedModelHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(out); err != nil {
		trace("model-response-error", map[string]any{"statusCode": resp.StatusCode, "message": err.Error()})
		return
	}
	if validationErr == nil && loadErr == nil {
		trace("models-injected", map[string]any{"officialCount": summary.officialCount, "customCount": summary.customCount, "customNames": summary.customNames, "customSlugs": summary.customSlugs, "containers": summary.containers, "indexPaths": summary.indexPaths})
	}
}

func decodeModelResponse(body []byte, encoding string) ([]byte, error) {
	encoding = strings.ToLower(strings.TrimSpace(strings.Split(encoding, ",")[0]))
	switch encoding {
	case "", "identity":
		return body, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("gzip 模型响应解码失败: %w", err)
		}
		defer reader.Close()
		return io.ReadAll(reader)
	case "deflate":
		reader, err := zlib.NewReader(bytes.NewReader(body))
		if err == nil {
			defer reader.Close()
			return io.ReadAll(reader)
		}
		raw := flate.NewReader(bytes.NewReader(body))
		defer raw.Close()
		decoded, rawErr := io.ReadAll(raw)
		if rawErr != nil {
			return nil, fmt.Errorf("deflate 模型响应解码失败: zlib=%v, raw=%w", err, rawErr)
		}
		return decoded, nil
	case "br":
		decoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(body)))
		if err != nil {
			return nil, fmt.Errorf("br 模型响应解码失败: %w", err)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("不支持的模型响应 Content-Encoding: %s", encoding)
	}
}

func copyDecodedModelHeaders(target, source http.Header) {
	for key, values := range source {
		lower := strings.ToLower(key)
		if lower == "content-encoding" || lower == "content-length" || lower == "transfer-encoding" {
			continue
		}
		for _, value := range values {
			target.Add(key, value)
		}
	}
	target.Set("Content-Type", "application/json; charset=utf-8")
}

func forwardRawModelResponse(w http.ResponseWriter, resp *http.Response, body []byte) {
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func saveModelStructureSnapshot(parsed map[string]any, summary modelInjectionSummary, statusCode int, encoding string, validationErr error) error {
	rootKeys := make([]string, 0, len(parsed))
	for key := range parsed {
		rootKeys = append(rootKeys, key)
	}
	sort.Strings(rootKeys)
	validation := "ok"
	if validationErr != nil {
		validation = "failed"
	}
	snapshot := map[string]any{
		"updatedAt":       time.Now().UTC().Format(time.RFC3339Nano),
		"statusCode":      statusCode,
		"contentEncoding": encoding,
		"rootFields":      rootKeys,
		"containers":      summary.containers,
		"indexes":         summary.indexPaths,
		"officialSample":  jsonShape(summary.officialSample, 0),
		"customSample":    jsonShape(summary.customSample, 0),
		"validation":      validation,
	}
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	dir := storage.StorageDir()
	if dir == "" {
		return nil
	}
	path := filepath.Join(dir, "model-response-structure.json")
	temp, err := os.CreateTemp(dir, ".model-response-structure-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(append(encoded, '\n')); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tempPath, path)
}

func jsonShape(value any, depth int) any {
	if depth >= 6 {
		return "object"
	}
	switch current := value.(type) {
	case nil:
		return "none"
	case map[string]any:
		shape := make(map[string]any, len(current))
		for key, item := range current {
			shape[key] = jsonShape(item, depth+1)
		}
		return shape
	case []any:
		shape := map[string]any{"type": "array", "length": len(current)}
		if len(current) > 0 {
			shape["item"] = jsonShape(current[0], depth+1)
		}
		return shape
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int64, json.Number:
		return "number"
	default:
		return fmt.Sprintf("%T", value)
	}
}
