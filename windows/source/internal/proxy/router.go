package proxy

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"antigravity-byok/internal/storage"
	"antigravity-byok/internal/upstream"
	"github.com/andybalholm/brotli"
)

const (
	googleHost    = "daily-cloudcode-pa.googleapis.com"
	googleBaseURL = "https://daily-cloudcode-pa.googleapis.com"
	maxRetries    = 3
	// Antigravity 1.23.x only recognises placeholder enum values M0-M150.
	// Unknown values are decoded as MODEL_UNSPECIFIED and disappear from the
	// built-in model picker.
	modelPlaceholderCount = 151
)

// Model-list injection and generation routing share these assignments.  They
// must change together: Antigravity may send a placeholder enum while older
// UI state still uses the corresponding slug.  A failed compatibility probe
// must therefore never replace either half of the currently active mapping.
var (
	modelAssignmentsMu          sync.RWMutex
	modelInjectionTransactionMu sync.Mutex
	allocatedPlaceholders       = map[string]string{}
	allocatedSlugs              = map[string]string{}
)

type modelRouteAssignments struct {
	placeholders map[string]string
	slugs        map[string]string
}

func copyModelRouteAssignmentMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

// commitModelRouteAssignments publishes a fully validated model-list mapping
// in one critical section.  Callers must not invoke it for an unknown or
// partially injected response.
func commitModelRouteAssignments(assignments modelRouteAssignments) {
	if assignments.placeholders == nil || assignments.slugs == nil {
		return
	}
	modelAssignmentsMu.Lock()
	allocatedPlaceholders = copyModelRouteAssignmentMap(assignments.placeholders)
	allocatedSlugs = copyModelRouteAssignmentMap(assignments.slugs)
	modelAssignmentsMu.Unlock()
}

func snapshotModelRouteAssignments() modelRouteAssignments {
	modelAssignmentsMu.RLock()
	defer modelAssignmentsMu.RUnlock()
	return modelRouteAssignments{
		placeholders: copyModelRouteAssignmentMap(allocatedPlaceholders),
		slugs:        copyModelRouteAssignmentMap(allocatedSlugs),
	}
}

// replaceModelRouteAssignmentsForTest isolates package-level routing state for
// regression tests.  It is deliberately not used by production code.
func replaceModelRouteAssignmentsForTest(assignments modelRouteAssignments) func() {
	previous := snapshotModelRouteAssignments()
	commitModelRouteAssignments(assignments)
	return func() { commitModelRouteAssignments(previous) }
}

// getModelSlug returns a stable routing slug for a model.
func getModelSlug(m storage.CustomModel) string {
	slug, _ := modelRouteFor(m, snapshotModelRouteAssignments())
	return slug
}

func baseModelSlug(m storage.CustomModel) string {
	src := m.ExternalModelName
	if src == "" {
		src = m.Name
	}
	src = strings.TrimPrefix(src, "models/")
	var b strings.Builder
	for _, r := range strings.ToLower(src) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "model"
	}
	return "custom-" + slug
}

func allocateModelSlugs(models []storage.CustomModel, usedModelIDs map[string]struct{}) map[string]string {
	assignments, _ := allocateModelSlugsWithExisting(models, usedModelIDs, modelRouteAssignments{})
	return assignments
}

func allocateModelSlugsWithExisting(models []storage.CustomModel, usedModelIDs map[string]struct{}, existing modelRouteAssignments) (map[string]string, error) {
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
		key := modelPlaceholderKey(model)
		slug := existing.slugs[key]
		if slug == "" {
			continue
		}
		if _, collision := used[slug]; collision {
			return nil, fmt.Errorf("原生模型响应与已激活模型 %s 的模型标识冲突", key)
		}
		assignments[key] = slug
		used[slug] = struct{}{}
	}
	for _, model := range ordered {
		key := modelPlaceholderKey(model)
		if assignments[key] != "" {
			continue
		}
		base := baseModelSlug(model)
		candidate := base
		for suffix := 2; ; suffix++ {
			if _, exists := used[candidate]; !exists {
				break
			}
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		assignments[key] = candidate
		used[candidate] = struct{}{}
	}
	return assignments, nil
}

func modelPlaceholderKey(m storage.CustomModel) string {
	if m.Name != "" {
		return m.Name
	}
	if m.ExternalModelName != "" {
		return m.ExternalModelName
	}
	return m.DisplayName
}

func modelPlaceholderHash(m storage.CustomModel) uint32 {
	src := strings.ToLower(m.DisplayName)
	if src == "" {
		src = strings.ToLower(modelPlaceholderKey(m))
	}
	var h uint32 = 5381
	for _, c := range src {
		h = (h << 5) + h + uint32(c)
	}
	return h
}

// getModelPlaceholder returns the placeholder allocated during the latest
// model-list injection, with a valid deterministic fallback for unit-level
// conversion calls.
func getModelPlaceholder(m storage.CustomModel) string {
	_, placeholder := modelRouteFor(m, snapshotModelRouteAssignments())
	return placeholder
}

// modelRouteFor resolves both identifiers from one assignment snapshot. A
// routing decision must never combine a slug from one injected picker with a
// placeholder from a later picker refresh.
func modelRouteFor(m storage.CustomModel, assignments modelRouteAssignments) (slug, placeholder string) {
	key := modelPlaceholderKey(m)
	slug = assignments.slugs[key]
	if slug == "" {
		slug = baseModelSlug(m)
	}
	placeholder = assignments.placeholders[key]
	if placeholder == "" {
		placeholder = fmt.Sprintf("MODEL_PLACEHOLDER_M%d", modelPlaceholderHash(m)%modelPlaceholderCount)
	}
	return slug, placeholder
}

// allocateModelPlaceholders selects valid enum values that do not collide with
// models already present in Google's response.  It is intentionally pure:
// assignments are published only after the complete injected response has
// passed picker validation.
func allocateModelPlaceholders(models []storage.CustomModel, officialModels map[string]any) map[string]string {
	assignments, _ := allocateModelPlaceholdersWithExisting(models, officialModels, modelRouteAssignments{})
	return assignments
}

// allocateModelPlaceholdersWithExisting preserves the live enum mapping for
// models that can still coexist with the current native response. Reassigning
// an already visible placeholder would make a stale picker select a different
// upstream model. If Google's newest response claims that enum instead, the
// caller must fail closed rather than silently pick another one.
func allocateModelPlaceholdersWithExisting(models []storage.CustomModel, officialModels map[string]any, existing modelRouteAssignments) (map[string]string, error) {
	used := make(map[string]struct{})
	for _, raw := range officialModels {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if modelID, ok := entry["model"].(string); ok && modelID != "" {
			used[modelID] = struct{}{}
		}
	}

	ordered := append([]storage.CustomModel(nil), models...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return modelPlaceholderKey(ordered[i]) < modelPlaceholderKey(ordered[j])
	})

	assignments := make(map[string]string, len(ordered))
	for _, model := range ordered {
		key := modelPlaceholderKey(model)
		placeholder := existing.placeholders[key]
		if placeholder == "" {
			continue
		}
		if !isSupportedModelPlaceholder(placeholder) {
			return nil, fmt.Errorf("已激活模型 %s 使用了无效占位符 %s", key, placeholder)
		}
		if _, collision := used[placeholder]; collision {
			return nil, fmt.Errorf("原生模型响应与已激活模型 %s 的占位符冲突", key)
		}
		assignments[key] = placeholder
		used[placeholder] = struct{}{}
	}

	for _, model := range ordered {
		key := modelPlaceholderKey(model)
		if assignments[key] != "" {
			continue
		}
		start := int(modelPlaceholderHash(model) % modelPlaceholderCount)
		for offset := 0; offset < modelPlaceholderCount; offset++ {
			candidate := fmt.Sprintf("MODEL_PLACEHOLDER_M%d", (start+offset)%modelPlaceholderCount)
			if _, exists := used[candidate]; exists {
				continue
			}
			assignments[key] = candidate
			used[candidate] = struct{}{}
			break
		}
	}

	return assignments, nil
}

func isSupportedModelPlaceholder(value string) bool {
	const prefix = "MODEL_PLACEHOLDER_M"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	index, err := strconv.Atoi(strings.TrimPrefix(value, prefix))
	return err == nil && index >= 0 && index < modelPlaceholderCount
}

// buildFakeModelEntry builds the JSON entry injected into the model list.
func buildFakeModelEntry(m storage.CustomModel, placeholder string) map[string]any {
	capabilities := storage.EffectiveCapabilities(m)
	mimeTypes := make(map[string]any, len(capabilities.SupportedMimeTypes))
	for _, mimeType := range capabilities.SupportedMimeTypes {
		mimeTypes[mimeType] = true
	}
	entry := map[string]any{
		"displayName":                  m.DisplayName,
		"description":                  m.Description,
		"recommended":                  true,
		"maxTokens":                    1048576,
		"maxOutputTokens":              65536,
		"tokenizerType":                "LLAMA_WITH_SPECIAL",
		"model":                        placeholder,
		"apiProvider":                  "API_PROVIDER_GOOGLE_GEMINI",
		"modelProvider":                "MODEL_PROVIDER_GOOGLE",
		"supportsCumulativeContext":    true,
		"supportsEstimateTokenCounter": true,
		"supportsImages":               capabilities.SupportsImages,
		"supportsAudio":                false,
		"supportsVideo":                false,
		"supportsFiles":                capabilities.SupportsFiles,
		"supportsToolCalls":            capabilities.SupportsToolCalls,
		"supportsThinking":             capabilities.SupportsThinking,
		"supportsWebSearch":            capabilities.SupportsWebSearch,
		"supportsImageGeneration":      capabilities.SupportsImageGeneration,
		// Newer Antigravity language servers use this ModelDetails capability
		// when they turn a native image-generation result into chat media.  The
		// value must stay coupled to the real, proxy-supported image capability:
		// declaring it for an ordinary text model makes the IDE offer a tool that
		// cannot produce an attachment, while omitting it can leave a valid image
		// result visible only to the tool runner instead of the conversation.
		"requiresImageOutputOutsideFunctionResponses": capabilities.SupportsImageGeneration,
		"supportedMimeTypes":                          mimeTypes,
	}
	// The exact field names are version-dependent in Antigravity. Keep the
	// canonical fields above, and provide these aliases for IDE builds that use
	// the newer capability schema. Unknown keys are ignored by older builds.
	entry["supportsTools"] = capabilities.SupportsToolCalls
	entry["supportsFileInput"] = capabilities.SupportsFiles
	return entry
}

var modelContainerKeys = []string{"models", "availableModels", "available_models"}
var modelWrapperKeys = []string{"response", "result", "data"}
var modelSortKeys = []string{"agentModelSorts", "battleModeModelSorts"}
var modelIDIndexKeys = map[string]bool{
	"tieredModelIds":      true,
	"availableModelIds":   true,
	"allowedModelIds":     true,
	"allowlistedModelIds": true,
	// Present in newer FetchAvailableModels responses. It is not a generic
	// picker index: only models that really support image generation belong in
	// it. Older Antigravity versions omit this field and continue unchanged.
	"imageGenerationModelIds": true,
}

type modelResponseRoot struct {
	path  string
	value map[string]any
}

type modelInjectionSummary struct {
	officialCount    int
	customCount      int
	customNames      []string
	customSlugs      []string
	containers       []string
	indexPaths       []string
	unsupportedShape bool
	officialSample   any
	customSample     any
	assignments      modelRouteAssignments
	assignmentErr    error
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
	if len(models) == 0 {
		return summary
	}
	// Validate the response shape before touching the process-wide slug and
	// placeholder assignments used by request routing. An unknown JSON payload
	// must be a complete no-op: a failed compatibility probe must not change
	// how an already-open Antigravity picker routes its selected model.
	hasSupportedContainer := false
	for _, root := range roots {
		for _, key := range modelContainerKeys {
			switch root.value[key].(type) {
			case map[string]any, []any:
				hasSupportedContainer = true
			}
		}
	}
	if !hasSupportedContainer {
		summary.unsupportedShape = true
		return summary
	}
	official := collectOfficialModelEntries(roots)
	usedModelIDs := collectUsedModelIDs(roots)
	existingAssignments := snapshotModelRouteAssignments()
	slugAssignments, slugErr := allocateModelSlugsWithExisting(models, usedModelIDs, existingAssignments)
	if slugErr != nil {
		summary.assignmentErr = slugErr
		return summary
	}
	assignments, placeholderErr := allocateModelPlaceholdersWithExisting(models, official, existingAssignments)
	if placeholderErr != nil {
		summary.assignmentErr = placeholderErr
		return summary
	}
	summary.assignments = modelRouteAssignments{placeholders: assignments, slugs: slugAssignments}
	slugs := make([]string, 0, len(models))
	imageGenerationSlugs := make([]string, 0, len(models))

	for _, model := range models {
		key := modelPlaceholderKey(model)
		if assignments[key] == "" || slugAssignments[key] == "" {
			continue
		}
		slug := slugAssignments[key]
		slugs = append(slugs, slug)
		if storage.EffectiveCapabilities(model).SupportsImageGeneration {
			imageGenerationSlugs = append(imageGenerationSlugs, slug)
		}
		summary.customNames = append(summary.customNames, modelDisplayName(model))
		summary.customSlugs = append(summary.customSlugs, slug)
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
				summary.officialCount = max(summary.officialCount, len(container))
				for _, entry := range container {
					if summary.officialSample == nil {
						summary.officialSample = entry
					}
					break
				}
				for _, model := range models {
					modelKey := modelPlaceholderKey(model)
					placeholder := assignments[modelKey]
					slug := slugAssignments[modelKey]
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
				summary.officialCount = max(summary.officialCount, len(container))
				if len(container) > 0 && summary.officialSample == nil {
					summary.officialSample = container[0]
				}
				injected := make([]any, 0, len(models))
				for _, model := range models {
					modelKey := modelPlaceholderKey(model)
					placeholder := assignments[modelKey]
					slug := slugAssignments[modelKey]
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

	if len(summary.containers) == 0 {
		// Do not invent a models/agentModelSorts schema for an unknown successful
		// response. A future Antigravity release may use a different payload that
		// happens to be JSON; adding guessed fields can make its Language Server
		// reject or silently reinterpret the response. The caller records the
		// explicit compatibility diagnostic and forwards the original shape.
		summary.unsupportedShape = true
		return summary
	}

	for _, root := range indexedRoots {
		summary.indexPaths = append(summary.indexPaths, addModelIndexes(root.value, root.path, slugs, imageGenerationSlugs)...)
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

func addModelIndexes(parsed map[string]any, rootPath string, modelIDs, imageGenerationModelIDs []string) []string {
	if len(modelIDs) == 0 {
		return nil
	}
	var paths []string
	for _, key := range modelSortKeys {
		raw, exists := parsed[key]
		if !exists {
			continue
		}
		updated, changed := addModelSortIDs(raw, modelIDs)
		if !changed {
			// A field with a familiar name is not enough evidence that this
			// Language Server uses the legacy sorter schema. Leave an unknown
			// value untouched; validation will fail the detached candidate rather
			// than returning a payload with a guessed replacement type.
			continue
		}
		parsed[key] = updated
		paths = append(paths, modelPath(rootPath, key)+".groups[].modelIds")
	}
	// Do not invent agentModelSorts for a response that merely happens to have a
	// models-shaped container.  The picker index is consumed by the Language
	// Server/renderer rather than by this HTTP endpoint alone, and an unknown
	// version may use a different proto field or visibility gate.  Validation
	// below will reject the candidate when no known, existing index receives the
	// model IDs, so the caller forwards the exact native response unchanged.
	for key := range modelIDIndexKeys {
		value, exists := parsed[key]
		if !exists {
			continue
		}
		ids := modelIDs
		if key == "imageGenerationModelIds" {
			ids = imageGenerationModelIDs
		}
		if len(ids) == 0 {
			continue
		}
		updated, changed := prependModelIDs(value, ids)
		if changed {
			parsed[key] = updated
			paths = append(paths, modelPath(rootPath, key))
		}
	}
	return paths
}

func addModelSortIDs(raw any, modelIDs []string) (any, bool) {
	sorts, ok := raw.([]any)
	if !ok {
		return raw, false
	}
	inserted := false
	for sortIndex, rawSort := range sorts {
		sortEntry, ok := rawSort.(map[string]any)
		if !ok {
			continue
		}
		groups, ok := sortEntry["groups"].([]any)
		if !ok {
			continue
		}
		groupsChanged := false
		for groupIndex, rawGroup := range groups {
			group, ok := rawGroup.(map[string]any)
			if !ok {
				continue
			}
			ids, ok := group["modelIds"].([]any)
			if !ok {
				continue
			}
			group["modelIds"] = prependUniqueModelIDs(ids, modelIDs)
			groups[groupIndex] = group
			groupsChanged = true
			inserted = true
		}
		if groupsChanged {
			sortEntry["groups"] = groups
			sorts[sortIndex] = sortEntry
		}
	}
	return sorts, inserted
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
			updated, itemChanged := prependModelIDs(item, modelIDs)
			if itemChanged {
				current[index] = updated
				changed = true
			}
		}
		return current, changed
	case map[string]any:
		changed := false
		for key, item := range current {
			updated, itemChanged := prependModelIDs(item, modelIDs)
			if itemChanged {
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
	if summary.unsupportedShape {
		return fmt.Errorf("上游模型响应不包含已支持的模型容器（models、availableModels 或 available_models）；为避免破坏未知 Antigravity 协议，未注入自定义模型")
	}
	if summary.assignmentErr != nil {
		return fmt.Errorf("为保持已打开模型选择器的路由一致性，未注入自定义模型: %w", summary.assignmentErr)
	}
	if summary.customCount != len(models) {
		return fmt.Errorf("只为 %d/%d 个自定义模型分配了有效标识", summary.customCount, len(models))
	}
	roots := collectModelResponseRoots(parsed)
	for _, model := range models {
		key := modelPlaceholderKey(model)
		slug := summary.assignments.slugs[key]
		placeholder := summary.assignments.placeholders[key]
		if slug == "" || placeholder == "" {
			return fmt.Errorf("注入后缺少 %s 的本次模型路由标识", key)
		}
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
			if strings.HasSuffix(lower, "modelids") || strings.HasSuffix(lower, "model_ids") ||
				key == "agentModelSorts" || key == "battleModeModelSorts" {
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

// handleFetchAvailableModels proxies fetchAvailableModels and injects custom models.
func handleFetchAvailableModels(w http.ResponseWriter, r *http.Request) {
	handleFetchAvailableModelsWithClient(w, r, &http.Client{Timeout: 30 * time.Second})
}

// handleFetchAvailableModelsWithClient contains the model-list path so it can
// be exercised with a local transport. The production entry point above keeps
// the upstream client private and time-bounded.
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		trace("model-response-error", map[string]any{
			"statusCode": resp.StatusCode, "encoding": resp.Header.Get("Content-Encoding"), "message": err.Error(),
		})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	encoding := resp.Header.Get("Content-Encoding")
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		trace("model-response-error", map[string]any{
			"statusCode": resp.StatusCode, "encoding": encoding, "contentType": resp.Header.Get("Content-Type"),
			"message": fmt.Sprintf("模型上游返回 HTTP %d", resp.StatusCode),
		})
		forwardRawModelResponse(w, resp, respBody)
		return
	}
	decoded, err := decodeModelResponse(respBody, encoding)
	if err != nil {
		trace("model-response-error", map[string]any{
			"statusCode": resp.StatusCode, "encoding": encoding, "message": err.Error(),
		})
		forwardRawModelResponse(w, resp, respBody)
		return
	}

	var decodedJSON any
	if err := json.Unmarshal(decoded, &decodedJSON); err != nil {
		trace("model-response-error", map[string]any{
			"statusCode": resp.StatusCode, "encoding": encoding, "message": fmt.Sprintf("模型响应 JSON 解析失败: %v", err),
		})
		forwardRawModelResponse(w, resp, respBody)
		return
	}
	parsed, ok := decodedJSON.(map[string]any)
	if !ok {
		trace("model-response-error", map[string]any{
			"statusCode": resp.StatusCode, "encoding": encoding, "message": "模型响应 JSON 根节点不是对象",
		})
		forwardRawModelResponse(w, resp, respBody)
		return
	}

	models, loadErr := storage.LoadEnabledModels()
	if loadErr != nil {
		trace("model-injection-error", map[string]any{"message": fmt.Sprintf("读取自定义模型失败: %v", loadErr)})
		models = nil
	}

	// Serialize snapshot -> allocation -> validation -> commit. Two concurrent
	// fetches must not calculate candidate mappings from the same old snapshot
	// and then publish them in a different order from the picker responses they
	// return to Antigravity.
	modelInjectionTransactionMu.Lock()
	// Build and validate the candidate on a detached JSON tree. The original
	// decoded response remains the exact fail-closed fallback, while the global
	// routing assignments remain untouched until the candidate is proven safe.
	candidate, cloneErr := cloneModelResponse(parsed)
	var summary modelInjectionSummary
	var validationErr error
	var out []byte
	var marshalErr error
	if cloneErr == nil {
		summary = injectCustomModels(candidate, models)
		validationErr = validateModelInjection(candidate, models, summary)
		if validationErr == nil {
			out, marshalErr = json.Marshal(candidate)
			if marshalErr == nil && len(models) > 0 {
				commitModelRouteAssignments(summary.assignments)
			}
		}
	}
	modelInjectionTransactionMu.Unlock()

	if cloneErr != nil {
		trace("model-injection-error", map[string]any{
			"message": fmt.Sprintf("无法创建模型注入候选副本: %v", cloneErr),
		})
		copyDecodedModelHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(decoded)
		return
	}
	if validationErr != nil {
		trace("model-injection-error", map[string]any{
			"configuredCount": len(models), "customCount": summary.customCount, "containers": summary.containers,
			"indexPaths": summary.indexPaths, "message": validationErr.Error(),
		})
	}
	if snapshotErr := saveModelStructureSnapshot(candidate, summary, resp.StatusCode, encoding, validationErr); snapshotErr != nil {
		trace("model-snapshot-error", map[string]any{"message": snapshotErr.Error()})
	}
	if validationErr != nil {
		// A failed post-injection validation means the proxy cannot prove that
		// this Language Server will accept the altered picker schema.  Never
		// hand it a partially injected response: that can leave the IDE with a
		// model map and indexes that disagree, which is worse than safely
		// showing only the native models for this compatibility probe.
		copyDecodedModelHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(decoded)
		return
	}

	if marshalErr != nil {
		trace("model-response-error", map[string]any{"statusCode": resp.StatusCode, "encoding": encoding, "message": marshalErr.Error()})
		http.Error(w, marshalErr.Error(), http.StatusBadGateway)
		return
	}
	copyDecodedModelHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(out); err != nil {
		trace("model-response-error", map[string]any{"statusCode": resp.StatusCode, "message": err.Error()})
		return
	}
	if validationErr == nil && loadErr == nil {
		trace("models-injected", map[string]any{
			"officialCount": summary.officialCount, "customCount": summary.customCount,
			"customNames": summary.customNames, "customSlugs": summary.customSlugs,
			"containers": summary.containers, "indexPaths": summary.indexPaths,
		})
	}
}

func cloneModelResponse(parsed map[string]any) (map[string]any, error) {
	encoded, err := json.Marshal(parsed)
	if err != nil {
		return nil, err
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil, err
	}
	return clone, nil
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

func addAgentModelID(parsed *map[string]any, modelID string) {
	addAgentModelIDs(*parsed, []string{modelID})
}

func addAgentModelIDs(parsed map[string]any, modelIDs []string) {
	if updated, changed := addModelSortIDs(parsed["agentModelSorts"], modelIDs); changed {
		parsed["agentModelSorts"] = updated
	}
}

// findModel returns the custom model matching a model ID or placeholder.
func findModel(modelID string) *storage.CustomModel {
	models, _ := storage.LoadEnabledModels()
	assignments := snapshotModelRouteAssignments()
	for _, m := range models {
		slug, placeholder := modelRouteFor(m, assignments)
		if modelID == m.Name || modelID == m.ExternalModelName ||
			modelID == slug || modelID == "models/"+slug || modelID == placeholder ||
			modelID == "models/"+placeholder ||
			modelID == strings.TrimPrefix(m.Name, "models/") {
			mc := m
			return &mc
		}
	}
	return nil
}

// resolveGenerationModel keeps an image source only for the internal image
// subrequest that follows a compatible custom-model turn. A normal native
// agent turn is an explicit model switch for that trajectory, so it clears a
// previously remembered custom source before being passed through to Gemini.
func resolveGenerationModel(modelID, requestID string) (customModel *storage.CustomModel, customMatched, nativeImageSource bool) {
	customModel = findModel(modelID)
	customMatched = customModel != nil
	if customMatched {
		return customModel, true, false
	}
	if isNativeImageGenerationRequestID(requestID) {
		if source := imageGenerationSourceForRequest(requestID); source != nil {
			return source, false, true
		}
		return nil, false, false
	}
	forgetImageGenerationSource(requestID)
	return nil, false, false
}

// handleGenerate routes a streamGenerateContent request. cleanPath is the
// already-normalised path, with any patcher prefix removed.
func handleGenerate(w http.ResponseWriter, r *http.Request, cleanPath string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}

	modelID, _ := req["model"].(string)
	if modelID == "" {
		if mid, ok := req["modelId"].(string); ok {
			modelID = mid
		}
	}

	requestID, _ := req["requestId"].(string)
	generationRequestID := requestID
	if requestID == "" {
		requestID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	customModel, customMatched, nativeImageSource := resolveGenerationModel(modelID, requestID)

	trace("generation-request", map[string]any{
		"requestId":         requestID,
		"model":             modelID,
		"customMatched":     customMatched,
		"nativeImageSource": nativeImageSource,
	})

	if customModel == nil {
		// Passthrough to Google
		passthroughRequest(w, r, body, cleanPath)
		return
	}

	geminiReq, _ := req["request"].(map[string]any)
	if geminiReq == nil {
		geminiReq = req
	}
	if nativeImageSource {
		// image_gen requests are addressed to Antigravity's internal Gemini
		// image model. This marker keeps the recovered upstream model on the
		// dedicated image route even though that payload has no model-specific
		// generationConfig field.
		geminiReq["wfNativeImageGeneration"] = true
	} else {
		rememberImageGenerationSource(requestID, customModel)
	}
	// The IDE can address the same saved custom model through its display name,
	// slug or placeholder. Guard the canonical saved name so an overlapping
	// retry using another alias cannot create a second upstream generation.
	guardModelID := modelID
	if customModel.Name != "" {
		guardModelID = customModel.Name
	} else if customModel.ExternalModelName != "" {
		guardModelID = customModel.ExternalModelName
	}
	releaseGeneration, accepted := reserveGeneration(guardModelID, generationRequestID, geminiReq)
	if !accepted {
		trace("generation-duplicate-suppressed", map[string]any{
			"requestId": requestID, "model": modelID,
		})
		http.Error(w, "相同请求仍在处理中，已阻止重复上游调用", http.StatusConflict)
		return
	}
	defer releaseGeneration()

	if customModel.Provider == "anthropic" {
		forwardAnthropic(w, r, customModel, geminiReq, requestID)
	} else {
		forwardOpenAI(w, r, customModel, geminiReq, requestID)
	}
}

// forwardOpenAI chooses the configured API surface. In automatic mode Chat
// Completions is deliberately the default: advertising a feature in the model
// picker must not silently attach hosted web/image tools to every ordinary
// chat turn. Responses is selected only for an actual attachment or an
// explicit native/locally requested Responses feature.
func forwardOpenAI(w http.ResponseWriter, incoming *http.Request, m *storage.CustomModel, geminiReq map[string]any, requestID string) {
	// A Codex OAuth access token is valid only against ChatGPT's Responses
	// surface. Account-pool metadata is copied into the selected model later,
	// so inspect the model binding before choosing the initial route too.
	if isOpenAICodexOAuthModel(m) {
		forwardOpenAIResponses(w, incoming, m, geminiReq, requestID, false)
		return
	}
	// Route an explicit image-generation turn to the preferred enabled image
	// model. The current supplier is preferred; if it has no image model, an
	// independently configured image supplier provides its own endpoint and
	// credentials. A directly selected image-only model always uses this route.
	directImageRequest := requestsDirectImageGeneration(geminiReq)
	directImageModelSelected := isDirectImageModelName(m.ExternalModelName)
	if directImageRequest || directImageModelSelected {
		imageModel := directOpenAIImageModel(m)
		if directImageModelSelected {
			imageModel = m
		}
		if imageModel != nil {
			forwardOpenAIImagesGeneration(w, incoming, m, imageModel, geminiReq, requestID)
			return
		}
		if directImageModelSelected {
			http.Error(w, "当前图片模型没有可用的 Images API 配置", http.StatusBadRequest)
			return
		}
	}
	config := upstream.ConfigFromModel(*m)
	style := upstream.EffectiveAPIStyle(config)
	needsResponses := requiresOpenAIResponses(geminiReq)
	if style == "responses" || (style == "auto" && needsResponses) {
		// A native image turn must never silently downgrade to text Chat
		// Completions, because the IDE then reports "no image generated".
		if fallback := forwardOpenAIResponses(w, incoming, m, geminiReq, requestID, style == "auto" && !directImageRequest); !fallback {
			return
		}
	}
	forwardOpenAIChat(w, incoming, m, geminiReq, requestID)
}

func requiresOpenAIResponses(gemini map[string]any) bool {
	if hasGeminiAttachment(gemini) {
		return true
	}
	if len(requestedResponsesBuiltinTools(gemini)) > 0 {
		return true
	}
	if explicitResponsesFeatureMap(gemini) {
		return true
	}
	for _, key := range []string{"generationConfig", "toolConfig", "responseConfig", "wfConfig"} {
		if config, ok := gemini[key].(map[string]any); ok && explicitResponsesFeatureMap(config) {
			return true
		}
	}
	return nativeResponsesToolRequested(gemini["tools"])
}

// requestedResponsesBuiltinTools identifies a concrete request to invoke a
// hosted Responses tool. It intentionally does not infer intent from a model
// capability or user prompt text: capabilities are only advertised to the
// IDE, while every outbound hosted tool must be requested by this turn.
func requestedResponsesBuiltinTools(gemini map[string]any) map[string]struct{} {
	requested := make(map[string]struct{}, 2)
	collectRequestedResponsesBuiltinTools(gemini, requested)
	for _, key := range []string{"generationConfig", "toolConfig", "responseConfig", "wfConfig"} {
		if config, ok := gemini[key].(map[string]any); ok {
			collectRequestedResponsesBuiltinTools(config, requested)
		}
	}
	collectRequestedNativeResponsesTools(gemini["tools"], requested)
	return requested
}

func collectRequestedResponsesBuiltinTools(config map[string]any, requested map[string]struct{}) {
	for key, value := range config {
		switch normalisedResponsesFeatureKey(key) {
		case "websearch", "websearchretrieval", "googlesearch", "googlesearchretrieval", "urlcontext":
			if responseFeatureEnabled(value) {
				requested[responseWebSearchTool] = struct{}{}
			}
		case "imagegeneration", "imagegenerationconfig", "imagegen", "generateimage", "wfnativeimagegeneration":
			if responseFeatureEnabled(value) {
				requested[responseImageGenerationTool] = struct{}{}
			}
		case "responsemodalities", "modalities":
			if responseOutputMediaRequested(value) {
				requested[responseImageGenerationTool] = struct{}{}
			}
		}
	}
}

func collectRequestedNativeResponsesTools(raw any, requested map[string]struct{}) {
	tools, _ := raw.([]any)
	for _, rawTool := range tools {
		tool, _ := rawTool.(map[string]any)
		if tool == nil {
			continue
		}
		collectRequestedResponsesBuiltinTools(tool, requested)
		switch normalisedResponsesFeatureKey(getString(tool, "type")) {
		case "websearch", "websearchpreview", "websearchretrieval", "googlesearch", "googlesearchretrieval", "urlcontext":
			requested[responseWebSearchTool] = struct{}{}
		case "imagegeneration", "imagegen", "generateimage":
			requested[responseImageGenerationTool] = struct{}{}
		}
	}
}

// explicitResponsesFeatureMap accepts the small set of explicit feature
// fields emitted by native clients and our own local integration. It does not
// inspect free text or function declaration names, so a normal IDE terminal
// tool schema still uses the lower-cost Chat Completions path.
func explicitResponsesFeatureMap(config map[string]any) bool {
	for key, value := range config {
		switch normalisedResponsesFeatureKey(key) {
		case "wfuseresponses", "useresponses", "responsesapi", "responseapi":
			if responseFeatureEnabled(value) {
				return true
			}
		case "websearch", "websearchretrieval", "googlesearch", "googlesearchretrieval", "urlcontext",
			"imagegeneration", "imagegenerationconfig", "imagegen", "generateimage", "wfnativeimagegeneration":
			if responseFeatureEnabled(value) {
				return true
			}
		case "responsemodalities", "modalities":
			if responseOutputMediaRequested(value) {
				return true
			}
		}
	}
	return false
}

func nativeResponsesToolRequested(raw any) bool {
	requested := make(map[string]struct{}, 2)
	collectRequestedNativeResponsesTools(raw, requested)
	return len(requested) > 0
}

func normalisedResponsesFeatureKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("_", "", "-", "", " ", "").Replace(key)
	return key
}

func isNativeResponsesToolType(kind string) bool {
	switch normalisedResponsesFeatureKey(kind) {
	case "websearch", "websearchpreview", "websearchretrieval", "googlesearch", "googlesearchretrieval",
		"urlcontext", "imagegeneration", "imagegen", "generateimage":
		return true
	default:
		return false
	}
}

func responseFeatureEnabled(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		value = strings.ToLower(strings.TrimSpace(value))
		return value != "" && value != "false" && value != "none" && value != "disabled" && value != "off"
	case []any:
		return len(value) > 0
	case map[string]any:
		// Native Gemini tool declarations commonly use an empty object, for
		// example {"googleSearch": {}}. The field's presence is the request.
		return true
	default:
		return true
	}
}

func responseOutputMediaRequested(value any) bool {
	var visit func(any) bool
	visit = func(current any) bool {
		switch current := current.(type) {
		case string:
			return strings.Contains(strings.ToLower(current), "image")
		case []any:
			for _, item := range current {
				if visit(item) {
					return true
				}
			}
		case map[string]any:
			for _, item := range current {
				if visit(item) {
					return true
				}
			}
		}
		return false
	}
	return visit(value)
}

func forwardOpenAIChatLegacy(w http.ResponseWriter, incoming *http.Request, m *storage.CustomModel, geminiReq map[string]any, requestID string) {
	openAIReq, err := toOpenAIRequestWithMedia(geminiReq, m.ExternalModelName)
	if err != nil {
		trace("openai-input-error", map[string]any{"requestId": requestID, "message": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	applyOpenAIChatReasoning(openAIReq, m)
	openAIReq["stream"] = true
	openAIReq["stream_options"] = map[string]any{"include_usage": true}
	cache := applyOpenAIPromptCaching(openAIReq, m, geminiReq)
	cacheEnabled := cache.enabled

	apiURL, err := upstream.ResolveChatCompletionsURLForConfig(upstream.ConfigFromModel(*m))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var lastErr error
	extraAttempts := 0
	for attempt := 1; attempt <= maxRetries+extraAttempts; attempt++ {
		body, _ := json.Marshal(openAIReq)
		trace("openai-upstream-request", map[string]any{
			"requestId": requestID, "attempt": attempt,
			"promptCache": cacheEnabled, "promptCacheExplicit": cacheEnabled && cache.explicit,
			"promptCacheKeyHash": strings.TrimPrefix(cache.key, "antigravity:"),
		})
		req, err := http.NewRequestWithContext(incoming.Context(), "POST", apiURL, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if err := upstream.ApplyCredentials(req, upstream.ConfigFromModel(*m)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			trace("openai-upstream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			if attempt < maxRetries+extraAttempts && incoming.Context().Err() == nil {
				delay := retryDelay(attempt, "")
				trace("openai-upstream-retry", map[string]any{"requestId": requestID, "attempt": attempt, "delayMs": delay.Milliseconds()})
				time.Sleep(delay)
				continue
			}
			break
		}
		lastErr = nil

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			trace("openai-upstream-error-response", map[string]any{
				"requestId":  requestID,
				"statusCode": resp.StatusCode,
				"body":       string(errBody[:min(len(errBody), 500)]),
			})
			if cacheEnabled && isUnsupportedCacheResponse(resp.StatusCode, string(errBody)) {
				rememberUnsupportedPromptCache("openai", m)
				stripOpenAIPromptCaching(openAIReq)
				cacheEnabled = false
				extraAttempts = 1
				trace("prompt-cache-fallback", map[string]any{
					"requestId": requestID, "provider": "openai", "statusCode": resp.StatusCode,
				})
				continue
			}
			if isRetryableStatus(resp.StatusCode) && attempt < maxRetries+extraAttempts {
				delay := retryDelay(attempt, resp.Header.Get("Retry-After"))
				trace("openai-upstream-retry", map[string]any{"requestId": requestID, "attempt": attempt, "statusCode": resp.StatusCode, "delayMs": delay.Milliseconds()})
				time.Sleep(delay)
				continue
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			w.Write(errBody)
			return
		}

		defer resp.Body.Close()
		streamOpenAIResponse(w, resp, requestID, attempt)
		return
	}

	if lastErr != nil {
		http.Error(w, lastErr.Error(), 502)
	}
}

func forwardOpenAIChat(w http.ResponseWriter, incoming *http.Request, m *storage.CustomModel, geminiReq map[string]any, requestID string) {
	baseRequest, err := toOpenAIRequestWithMedia(geminiReq, m.ExternalModelName)
	if err != nil {
		trace("openai-input-error", map[string]any{"requestId": requestID, "message": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	applyOpenAIChatReasoning(baseRequest, m)
	baseRequest["stream"] = true
	baseRequest["stream_options"] = map[string]any{"include_usage": true}
	cache := applyOpenAIPromptCaching(baseRequest, m, geminiReq)
	cacheEnabled := cache.enabled

	policy := currentStreamRecoveryPolicy()
	writer := newDownstreamSSEWriter(w)
	client := &http.Client{Timeout: upstreamStreamTimeout}
	requestBody := baseRequest
	lastModelVersion, lastResponseID := "", ""
	reconnects := 0
	cacheFallbackUsed := false
	excludedAccounts := map[string]struct{}{}
	lastRejectedStatus := 0
	lastRejectedBody := ""

	for attempt := 1; ; attempt++ {
		attemptModel, lease, err := acquireAttemptModel(m, excludedAccounts)
		if err != nil {
			trace("openai-account-pool-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			if lastRejectedStatus != 0 && !writer.committed {
				if isRetryableStatus(lastRejectedStatus) || isTransientProviderRejection(lastRejectedStatus, lastRejectedBody) {
					writeRecoverableTurnStop(writer, "openai", requestID, lastModelVersion, "第三方上游的可用账户均暂时不可用，本轮未生成内容。请稍后再发送一次，或切换同模型的其他可用上游。", reconnects)
				} else {
					writeRejectedTurnStop(writer, "openai", requestID, lastModelVersion, lastRejectedStatus, lastRejectedBody)
				}
				return
			}
			reconnects++
			if waitForAccountPool(incoming.Context(), writer, policy, "openai", requestID, err, reconnects) {
				continue
			}
			if _, temporary := storage.AccountPoolRetryAfter(err); temporary && !writer.committed {
				writeRecoverableTurnStop(writer, "openai", requestID, lastModelVersion, "上游账户正在冷却或繁忙，本轮未生成内容。请稍后再发送一次，或切换同模型的其他可用上游。", reconnects)
				return
			}
			if writer.committed {
				writeRecoveredStreamStop(writer, requestID, lastModelVersion, lastResponseID, reconnects, false)
			} else {
				http.Error(w, accountPoolError("OpenAI", err), http.StatusServiceUnavailable)
			}
			return
		}
		attemptConfig := upstream.ConfigFromModel(*attemptModel)
		if upstream.IsOpenAICodexOAuth(attemptConfig) {
			// Account metadata can change after forwardOpenAI made its routing
			// decision. This second guard is intentionally immediately before
			// URL/credential construction: a Codex OAuth token must never reach
			// a Chat Completions endpoint.
			releaseAttemptSuccess(lease)
			forwardOpenAIResponses(w, incoming, m, geminiReq, requestID, false)
			return
		}
		apiURL, err := upstream.ResolveChatCompletionsURLForConfig(attemptConfig)
		if err != nil {
			releaseAttemptSuccess(lease)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body, _ := json.Marshal(requestBody)
		trace("openai-upstream-request", map[string]any{
			"requestId": requestID, "attempt": attempt,
			"promptCache": cacheEnabled, "promptCacheExplicit": cacheEnabled && cache.explicit,
			"promptCacheKeyHash": strings.TrimPrefix(cache.key, "antigravity:"),
			"accountId": func() string {
				if lease == nil {
					return ""
				}
				return lease.ID
			}(),
		})
		req, err := http.NewRequestWithContext(incoming.Context(), http.MethodPost, apiURL, bytes.NewReader(body))
		if err != nil {
			releaseAttemptSuccess(lease)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if err := upstream.ApplyCredentials(req, attemptConfig); err != nil {
			releaseAttemptSuccess(lease)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp, err := client.Do(req)
		if err != nil {
			releaseAttemptFailure(lease, 0, "", err.Error())
			excludeFailedAttempt(excludedAccounts, lease)
			trace("openai-upstream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			if incoming.Context().Err() != nil {
				return
			}
			writeUncertainUpstreamFailure(writer, "openai", requestID, lastModelVersion, lastResponseID, reconnects, false, "无法确认上游是否已接收请求："+err.Error())
			return
		}
		observeAttemptQuota(lease, "openai", resp)

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			retryAfter := resp.Header.Get("Retry-After")
			trace("openai-upstream-error-response", map[string]any{
				"requestId":  requestID,
				"statusCode": resp.StatusCode,
				"body":       string(errBody[:min(len(errBody), 500)]),
			})
			if cacheEnabled && !cacheFallbackUsed && !writer.committed && isUnsupportedCacheResponse(resp.StatusCode, string(errBody)) {
				releaseAttemptSuccess(lease)
				rememberUnsupportedPromptCache("openai", m)
				stripOpenAIPromptCaching(baseRequest)
				cacheEnabled = false
				cacheFallbackUsed = true
				trace("prompt-cache-fallback", map[string]any{"requestId": requestID, "provider": "openai", "statusCode": resp.StatusCode})
				requestBody = baseRequest
				continue
			}
			if shouldFailOverAccount(lease, resp.StatusCode, string(errBody)) {
				retrySameAccount := shouldRetrySameAccount(lease, resp.StatusCode, string(errBody))
				lastRejectedStatus, lastRejectedBody = resp.StatusCode, string(errBody)
				reconnects++
				mayRetry := canRetryRejectedRequest(writer, policy, reconnects)
				if mayRetry && retrySameAccount {
					if lease != nil {
						lease.Release()
					}
				} else {
					releaseAttemptFailure(lease, resp.StatusCode, retryAfter, string(errBody))
					if !retrySameAccount {
						excludeFailedAttempt(excludedAccounts, lease)
					}
				}
				if mayRetry && waitForRejectedRequestRetry(incoming.Context(), policy, "openai", requestID, fmt.Sprintf("http-%d", resp.StatusCode), retryAfter, reconnects) {
					requestBody = baseRequest
					continue
				}
				if incoming.Context().Err() != nil {
					return
				}
				if isRetryableStatus(resp.StatusCode) || isTransientProviderRejection(resp.StatusCode, string(errBody)) {
					writeRecoverableTurnStop(writer, "openai", requestID, lastModelVersion, "第三方上游暂时没有可用线路，本轮未生成内容。请稍后再发送一次，或切换同模型的其他可用上游。", reconnects)
					return
				}
				returnRejectedUpstreamError(w, resp.StatusCode, errBody)
				return
			}
			releaseAttemptFailure(lease, resp.StatusCode, retryAfter, string(errBody))
			if incoming.Context().Err() != nil {
				return
			}
			if writer.committed {
				writeRecoveredStreamStop(writer, requestID, lastModelVersion, lastResponseID, reconnects, false)
				return
			}
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || (resp.StatusCode == http.StatusNotFound && isModelRouteRejection(string(errBody))) {
				writeRejectedTurnStop(writer, "openai", requestID, lastModelVersion, resp.StatusCode, string(errBody))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(errBody)
			return
		}

		outcome := streamOpenAIAttempt(writer, resp, requestID, attempt, accountUsageTraceForAttempt(lease, attemptModel, "openai", attempt))
		resp.Body.Close()
		if outcome.responseID != "" {
			lastResponseID = outcome.responseID
		}
		if outcome.modelVersion != "" {
			lastModelVersion = outcome.modelVersion
		}
		if outcome.finished {
			releaseAttemptSuccess(lease)
			return
		}
		reason := "incomplete-stream"
		if outcome.err != nil {
			reason = outcome.err.Error()
		}
		releaseAttemptFailure(lease, 0, "", reason)
		excludeFailedAttempt(excludedAccounts, lease)
		if incoming.Context().Err() != nil {
			return
		}
		writeUncertainUpstreamFailure(writer, "openai", requestID, lastModelVersion, lastResponseID, reconnects, outcome.unsafeOutput, "上游流未正常完成："+reason)
		return
	}
}

// forwardOpenAIResponses returns true only when the caller may retry the same
// request through Chat Completions because the upstream does not expose
// /responses. It never falls back after a semantic 4xx, which would hide a
// model capability/configuration error from the user.
func forwardOpenAIResponsesLegacy(w http.ResponseWriter, incoming *http.Request, m *storage.CustomModel, geminiReq map[string]any, requestID string, allowFallback bool) bool {
	requestBody, err := toOpenAIResponsesRequest(geminiReq, m.ExternalModelName, m)
	if err != nil {
		trace("responses-input-error", map[string]any{"requestId": requestID, "message": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	applyOpenAIResponsesReasoning(requestBody, m)
	apiURL, err := upstream.ResolveResponsesURLForConfig(upstream.ConfigFromModel(*m))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		body, _ := json.Marshal(requestBody)
		trace("responses-upstream-request", map[string]any{"requestId": requestID, "attempt": attempt})
		req, err := http.NewRequestWithContext(incoming.Context(), http.MethodPost, apiURL, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		if err := upstream.ApplyCredentials(req, upstream.ConfigFromModel(*m)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries && incoming.Context().Err() == nil {
				time.Sleep(retryDelay(attempt, ""))
				continue
			}
			break
		}
		lastErr = nil
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			trace("responses-upstream-error-response", map[string]any{"requestId": requestID, "statusCode": resp.StatusCode, "body": string(errBody[:min(len(errBody), 500)])})
			if allowFallback && upstream.CanFallbackToChat(resp.StatusCode) {
				trace("responses-chat-fallback", map[string]any{"requestId": requestID, "statusCode": resp.StatusCode})
				return true
			}
			if isRetryableStatus(resp.StatusCode) && attempt < maxRetries {
				time.Sleep(retryDelay(attempt, resp.Header.Get("Retry-After")))
				continue
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(errBody)
			return false
		}
		defer resp.Body.Close()
		streamOpenAIResponsesResponse(w, resp, requestID, attempt)
		return false
	}
	if lastErr != nil {
		http.Error(w, lastErr.Error(), http.StatusBadGateway)
	}
	return false
}

func forwardOpenAIResponses(w http.ResponseWriter, incoming *http.Request, m *storage.CustomModel, geminiReq map[string]any, requestID string, allowFallback bool) bool {
	conversionModel := openAICodexResponsesConversionModel(m)
	baseRequest, err := toOpenAIResponsesRequest(geminiReq, m.ExternalModelName, conversionModel)
	if err != nil {
		trace("responses-input-error", map[string]any{"requestId": requestID, "message": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	applyOpenAIResponsesReasoning(baseRequest, m)
	policy := currentStreamRecoveryPolicy()
	writer := newDownstreamSSEWriter(w)
	client := &http.Client{Timeout: upstreamStreamTimeout}
	lastModelVersion, lastResponseID := "", ""
	reconnects := 0
	excludedAccounts := map[string]struct{}{}
	lastRejectedStatus := 0
	lastRejectedBody := ""

	for attempt := 1; ; attempt++ {
		attemptModel, lease, err := acquireAttemptModel(m, excludedAccounts)
		if err != nil {
			trace("responses-account-pool-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			if lastRejectedStatus != 0 && !writer.committed {
				if isRetryableStatus(lastRejectedStatus) || isTransientProviderRejection(lastRejectedStatus, lastRejectedBody) {
					writeRecoverableTurnStop(writer, "responses", requestID, lastModelVersion, "第三方上游的可用账户均暂时不可用，本轮未生成内容。请稍后再发送一次，或切换同模型的其他可用上游。", reconnects)
				} else {
					writeRejectedTurnStop(writer, "responses", requestID, lastModelVersion, lastRejectedStatus, lastRejectedBody)
				}
				return false
			}
			reconnects++
			if waitForAccountPool(incoming.Context(), writer, policy, "responses", requestID, err, reconnects) {
				continue
			}
			if _, temporary := storage.AccountPoolRetryAfter(err); temporary && !writer.committed {
				writeRecoverableTurnStop(writer, "responses", requestID, lastModelVersion, "上游账户正在冷却或繁忙，本轮未生成内容。请稍后再发送一次，或切换同模型的其他可用上游。", reconnects)
				return false
			}
			if writer.committed {
				writeRecoveredStreamStop(writer, requestID, lastModelVersion, lastResponseID, reconnects, false)
			} else {
				http.Error(w, accountPoolError("OpenAI", err), http.StatusServiceUnavailable)
			}
			return false
		}
		attemptConfig := upstream.ConfigFromModel(*attemptModel)
		codexOAuth := upstream.IsOpenAICodexOAuth(attemptConfig)
		if codexOAuth {
			// Never send a Codex access token to Chat Completions, even if this
			// model entered through generic automatic routing.
			allowFallback = false
		}
		apiURL, err := upstream.ResolveResponsesURLForConfig(attemptConfig)
		if err != nil {
			releaseAttemptSuccess(lease)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return false
		}
		// Compatibility is scoped to the selected account/endpoint. A pooled
		// account which rejects hosted tools must not disable them for another
		// account that may support them.
		requestBody, suppressedBuiltinTools := responseRequestForModel(baseRequest, attemptModel)
		if codexOAuth {
			// Preserve every image and tool requested by Antigravity. Codex has
			// its own Responses contract, rather than the gateway compatibility
			// cache used for generic OpenAI-compatible endpoints.
			requestBody = normalizeOpenAICodexResponsesRequest(baseRequest)
			suppressedBuiltinTools = nil
		}
		if len(suppressedBuiltinTools) > 0 {
			trace("responses-builtin-tools-suppressed", map[string]any{
				"requestId": requestID, "tools": suppressedBuiltinTools,
			})
		}
		body, _ := json.Marshal(requestBody)
		trace("responses-upstream-request", map[string]any{"requestId": requestID, "attempt": attempt, "accountId": func() string {
			if lease == nil {
				return ""
			}
			return lease.ID
		}()})
		req, err := http.NewRequestWithContext(incoming.Context(), http.MethodPost, apiURL, bytes.NewReader(body))
		if err != nil {
			releaseAttemptSuccess(lease)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		if err := upstream.ApplyCredentials(req, attemptConfig); err != nil {
			releaseAttemptSuccess(lease)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return false
		}

		resp, err := client.Do(req)
		if err != nil {
			releaseAttemptFailure(lease, 0, "", err.Error())
			excludeFailedAttempt(excludedAccounts, lease)
			trace("responses-upstream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			if incoming.Context().Err() != nil {
				return false
			}
			writeUncertainUpstreamFailure(writer, "responses", requestID, lastModelVersion, lastResponseID, reconnects, false, "无法确认上游是否已接收请求："+err.Error())
			return false
		}
		observeAttemptQuota(lease, "responses", resp)

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			retryAfter := resp.Header.Get("Retry-After")
			trace("responses-upstream-error-response", map[string]any{"requestId": requestID, "statusCode": resp.StatusCode, "body": string(errBody[:min(len(errBody), 500)])})
			if !codexOAuth {
				if rejectedTools := rejectedResponsesBuiltinTools(resp.StatusCode, string(errBody), requestBody); len(rejectedTools) > 0 && !writer.committed {
					// A concrete 4xx validation response proves this request did not
					// begin a generation. It is therefore safe to retry once with
					// only the rejected optional hosted tools removed.
					releaseAttemptSuccess(lease)
					rememberUnsupportedResponsesBuiltinTools(attemptModel, rejectedTools)
					trace("responses-builtin-tools-fallback", map[string]any{
						"requestId": requestID, "statusCode": resp.StatusCode, "tools": responseBuiltinToolNames(rejectedTools),
					})
					continue
				}
			}
			if allowFallback && !writer.committed && upstream.CanFallbackToChat(resp.StatusCode) {
				releaseAttemptSuccess(lease)
				trace("responses-chat-fallback", map[string]any{"requestId": requestID, "statusCode": resp.StatusCode})
				return true
			}
			if shouldFailOverAccount(lease, resp.StatusCode, string(errBody)) {
				retrySameAccount := shouldRetrySameAccount(lease, resp.StatusCode, string(errBody))
				lastRejectedStatus, lastRejectedBody = resp.StatusCode, string(errBody)
				reconnects++
				mayRetry := canRetryRejectedRequest(writer, policy, reconnects)
				if mayRetry && retrySameAccount {
					if lease != nil {
						lease.Release()
					}
				} else {
					releaseAttemptFailure(lease, resp.StatusCode, retryAfter, string(errBody))
					if !retrySameAccount {
						excludeFailedAttempt(excludedAccounts, lease)
					}
				}
				if mayRetry && waitForRejectedRequestRetry(incoming.Context(), policy, "responses", requestID, fmt.Sprintf("http-%d", resp.StatusCode), retryAfter, reconnects) {
					continue
				}
				if incoming.Context().Err() != nil {
					return false
				}
				if isRetryableStatus(resp.StatusCode) || isTransientProviderRejection(resp.StatusCode, string(errBody)) {
					writeRecoverableTurnStop(writer, "responses", requestID, lastModelVersion, "第三方上游暂时没有可用线路，本轮未生成内容。请稍后再发送一次，或切换同模型的其他可用上游。", reconnects)
					return false
				}
				returnRejectedUpstreamError(w, resp.StatusCode, errBody)
				return false
			}
			releaseAttemptFailure(lease, resp.StatusCode, retryAfter, string(errBody))
			if incoming.Context().Err() != nil {
				return false
			}
			if writer.committed {
				writeRecoveredStreamStop(writer, requestID, lastModelVersion, lastResponseID, reconnects, false)
				return false
			}
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || (resp.StatusCode == http.StatusNotFound && isModelRouteRejection(string(errBody))) {
				writeRejectedTurnStop(writer, "responses", requestID, lastModelVersion, resp.StatusCode, string(errBody))
				return false
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(errBody)
			return false
		}

		outcome := streamOpenAIResponsesAttempt(writer, resp, requestID, attempt, accountUsageTraceForAttempt(lease, attemptModel, "responses", attempt))
		resp.Body.Close()
		if outcome.responseID != "" {
			lastResponseID = outcome.responseID
		}
		if outcome.modelVersion != "" {
			lastModelVersion = outcome.modelVersion
		}
		if outcome.finished {
			releaseAttemptSuccess(lease)
			return false
		}
		reason := "incomplete-stream"
		if outcome.err != nil {
			reason = outcome.err.Error()
		}
		releaseAttemptFailure(lease, 0, "", reason)
		excludeFailedAttempt(excludedAccounts, lease)
		if incoming.Context().Err() != nil {
			return false
		}
		writeUncertainUpstreamFailure(writer, "responses", requestID, lastModelVersion, lastResponseID, reconnects, outcome.unsafeOutput, "上游流未正常完成："+reason)
		return false
	}
}

func responseReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "minimal", "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func resolveOpenAIChatCompletionsURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		base := strings.TrimRight(trimmed, "/")
		if strings.HasSuffix(base, "/chat/completions") {
			return base
		}
		if strings.HasSuffix(base, "/v1") {
			return base + "/chat/completions"
		}
		return base + "/v1/chat/completions"
	}

	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/chat/completions") {
		parsed.Path = path
		return parsed.String()
	}
	if path == "" {
		parsed.Path = "/v1/chat/completions"
	} else {
		parsed.Path = path + "/chat/completions"
	}
	return parsed.String()
}

// forwardAnthropic translates and forwards to Anthropic Messages API.
func forwardAnthropicLegacy(w http.ResponseWriter, incoming *http.Request, m *storage.CustomModel, geminiReq map[string]any, requestID string) {
	anthReq, err := toAnthropicRequestWithMedia(geminiReq, m.ExternalModelName)
	if err != nil {
		trace("anthropic-input-error", map[string]any{"requestId": requestID, "message": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	applyAnthropicReasoning(anthReq, m)
	breakpointCount := applyAnthropicPromptCachingForModel(anthReq, m)
	cacheEnabled := breakpointCount > 0

	apiURL, err := upstream.ResolveAnthropicMessagesURLForConfig(upstream.ConfigFromModel(*m))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	var lastErr error
	extraAttempts := 0
	for attempt := 1; attempt <= maxRetries+extraAttempts; attempt++ {
		body, _ := json.Marshal(anthReq)
		trace("anthropic-upstream-request", map[string]any{
			"requestId": requestID, "attempt": attempt,
			"promptCache": cacheEnabled, "promptCacheBreakpoints": func() int {
				if cacheEnabled {
					return breakpointCount
				}
				return 0
			}(),
		})
		req, err := http.NewRequestWithContext(incoming.Context(), "POST", apiURL, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if err := upstream.ApplyCredentials(req, upstream.ConfigFromModel(*m)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			trace("anthropic-upstream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			if attempt < maxRetries+extraAttempts && incoming.Context().Err() == nil {
				delay := retryDelay(attempt, "")
				trace("anthropic-upstream-retry", map[string]any{"requestId": requestID, "attempt": attempt, "delayMs": delay.Milliseconds()})
				time.Sleep(delay)
				continue
			}
			break
		}
		lastErr = nil

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			trace("anthropic-upstream-error-response", map[string]any{
				"requestId": requestID, "statusCode": resp.StatusCode,
				"body": string(errBody[:min(len(errBody), 500)]),
			})
			if cacheEnabled && isUnsupportedCacheResponse(resp.StatusCode, string(errBody)) {
				rememberUnsupportedPromptCache("anthropic", m)
				stripAnthropicPromptCaching(anthReq)
				cacheEnabled = false
				extraAttempts = 1
				trace("prompt-cache-fallback", map[string]any{
					"requestId": requestID, "provider": "anthropic", "statusCode": resp.StatusCode,
				})
				continue
			}
			if isRetryableStatus(resp.StatusCode) && attempt < maxRetries+extraAttempts {
				delay := retryDelay(attempt, resp.Header.Get("Retry-After"))
				trace("anthropic-upstream-retry", map[string]any{"requestId": requestID, "attempt": attempt, "statusCode": resp.StatusCode, "delayMs": delay.Milliseconds()})
				time.Sleep(delay)
				continue
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			w.Write(errBody)
			return
		}

		defer resp.Body.Close()
		streamAnthropicResponse(w, resp, requestID, attempt)
		return
	}
	if lastErr != nil {
		http.Error(w, lastErr.Error(), 502)
	}
}

func forwardAnthropic(w http.ResponseWriter, incoming *http.Request, m *storage.CustomModel, geminiReq map[string]any, requestID string) {
	baseRequest, err := toAnthropicRequestWithMedia(geminiReq, m.ExternalModelName)
	if err != nil {
		trace("anthropic-input-error", map[string]any{"requestId": requestID, "message": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	applyAnthropicReasoning(baseRequest, m)
	breakpointCount := applyAnthropicPromptCachingForModel(baseRequest, m)
	cacheEnabled := breakpointCount > 0

	policy := currentStreamRecoveryPolicy()
	writer := newDownstreamSSEWriter(w)
	client := &http.Client{Timeout: upstreamStreamTimeout}
	requestBody := baseRequest
	lastModelVersion, lastResponseID := "", ""
	reconnects := 0
	cacheFallbackUsed := false
	excludedAccounts := map[string]struct{}{}
	lastRejectedStatus := 0
	lastRejectedBody := ""

attemptLoop:
	for attempt := 1; ; attempt++ {
		attemptModel, lease, err := acquireAttemptModel(m, excludedAccounts)
		if err != nil {
			trace("anthropic-account-pool-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			if lastRejectedStatus != 0 && !writer.committed {
				if isRetryableStatus(lastRejectedStatus) || isTransientProviderRejection(lastRejectedStatus, lastRejectedBody) {
					writeRecoverableTurnStop(writer, "anthropic", requestID, lastModelVersion, "第三方上游的可用账户均暂时不可用，本轮未生成内容。请稍后再发送一次，或切换同模型的其他可用上游。", reconnects)
				} else {
					writeRejectedTurnStop(writer, "anthropic", requestID, lastModelVersion, lastRejectedStatus, lastRejectedBody)
				}
				return
			}
			reconnects++
			if waitForAccountPool(incoming.Context(), writer, policy, "anthropic", requestID, err, reconnects) {
				continue
			}
			if _, temporary := storage.AccountPoolRetryAfter(err); temporary && !writer.committed {
				writeRecoverableTurnStop(writer, "anthropic", requestID, lastModelVersion, "上游账户正在冷却或繁忙，本轮未生成内容。请稍后再发送一次，或切换同模型的其他可用上游。", reconnects)
				return
			}
			if writer.committed {
				writeRecoveredStreamStop(writer, requestID, lastModelVersion, lastResponseID, reconnects, false)
			} else {
				http.Error(w, accountPoolError("Claude", err), http.StatusServiceUnavailable)
			}
			return
		}
		attemptConfig := upstream.ConfigFromModel(*attemptModel)
		apiURLs, err := upstream.ResolveAnthropicMessageCandidates(attemptConfig)
		if err != nil {
			releaseAttemptSuccess(lease)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body, _ := json.Marshal(requestBody)
		for endpointIndex, apiURL := range apiURLs {
			trace("anthropic-upstream-request", map[string]any{
				"requestId": requestID, "attempt": attempt, "endpointCandidate": endpointIndex + 1,
				"accountId": func() string {
					if lease == nil {
						return ""
					}
					return lease.ID
				}(),
				"promptCache": cacheEnabled, "promptCacheBreakpoints": func() int {
					if cacheEnabled {
						return breakpointCount
					}
					return 0
				}(),
			})
			req, err := http.NewRequestWithContext(incoming.Context(), http.MethodPost, apiURL, bytes.NewReader(body))
			if err != nil {
				releaseAttemptSuccess(lease)
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			if err := upstream.ApplyCredentials(req, attemptConfig); err != nil {
				releaseAttemptSuccess(lease)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			resp, err := client.Do(req)
			if err != nil {
				releaseAttemptFailure(lease, 0, "", err.Error())
				excludeFailedAttempt(excludedAccounts, lease)
				trace("anthropic-upstream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
				if incoming.Context().Err() != nil {
					return
				}
				writeUncertainUpstreamFailure(writer, "anthropic", requestID, lastModelVersion, lastResponseID, reconnects, false, "无法确认上游是否已接收请求："+err.Error())
				return
			}
			observeAttemptQuota(lease, "anthropic", resp)

			if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
				errBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				retryAfter := resp.Header.Get("Retry-After")
				trace("anthropic-upstream-error-response", map[string]any{
					"requestId": requestID, "statusCode": resp.StatusCode,
					"body": string(errBody[:min(len(errBody), 500)]),
				})
				if endpointIndex+1 < len(apiURLs) && upstream.CanFallbackToChat(resp.StatusCode) {
					trace("anthropic-compatible-endpoint-fallback", map[string]any{"requestId": requestID, "statusCode": resp.StatusCode})
					continue
				}
				if cacheEnabled && !cacheFallbackUsed && !writer.committed && isUnsupportedCacheResponse(resp.StatusCode, string(errBody)) {
					releaseAttemptSuccess(lease)
					rememberUnsupportedPromptCache("anthropic", m)
					stripAnthropicPromptCaching(baseRequest)
					cacheEnabled = false
					cacheFallbackUsed = true
					trace("prompt-cache-fallback", map[string]any{"requestId": requestID, "provider": "anthropic", "statusCode": resp.StatusCode})
					requestBody = baseRequest
					continue attemptLoop
				}
				if shouldFailOverAccount(lease, resp.StatusCode, string(errBody)) {
					retrySameAccount := shouldRetrySameAccount(lease, resp.StatusCode, string(errBody))
					lastRejectedStatus, lastRejectedBody = resp.StatusCode, string(errBody)
					reconnects++
					mayRetry := canRetryRejectedRequest(writer, policy, reconnects)
					if mayRetry && retrySameAccount {
						if lease != nil {
							lease.Release()
						}
					} else {
						releaseAttemptFailure(lease, resp.StatusCode, retryAfter, string(errBody))
						if !retrySameAccount {
							excludeFailedAttempt(excludedAccounts, lease)
						}
					}
					if mayRetry && waitForRejectedRequestRetry(incoming.Context(), policy, "anthropic", requestID, fmt.Sprintf("http-%d", resp.StatusCode), retryAfter, reconnects) {
						requestBody = baseRequest
						continue attemptLoop
					}
					if incoming.Context().Err() != nil {
						return
					}
					if isRetryableStatus(resp.StatusCode) || isTransientProviderRejection(resp.StatusCode, string(errBody)) {
						writeRecoverableTurnStop(writer, "anthropic", requestID, lastModelVersion, "第三方上游暂时没有可用线路，本轮未生成内容。请稍后再发送一次，或切换同模型的其他可用上游。", reconnects)
						return
					}
					returnRejectedUpstreamError(w, resp.StatusCode, errBody)
					return
				}
				releaseAttemptFailure(lease, resp.StatusCode, retryAfter, string(errBody))
				if incoming.Context().Err() != nil {
					return
				}
				if writer.committed {
					writeRecoveredStreamStop(writer, requestID, lastModelVersion, lastResponseID, reconnects, false)
					return
				}
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || (resp.StatusCode == http.StatusNotFound && isModelRouteRejection(string(errBody))) {
					writeRejectedTurnStop(writer, "anthropic", requestID, lastModelVersion, resp.StatusCode, string(errBody))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.StatusCode)
				_, _ = w.Write(errBody)
				return
			}

			outcome := streamAnthropicAttempt(writer, resp, requestID, attempt, accountUsageTraceForAttempt(lease, attemptModel, "anthropic", attempt))
			resp.Body.Close()
			if outcome.responseID != "" {
				lastResponseID = outcome.responseID
			}
			if outcome.modelVersion != "" {
				lastModelVersion = outcome.modelVersion
			}
			if outcome.finished {
				releaseAttemptSuccess(lease)
				return
			}
			reason := "incomplete-stream"
			if outcome.err != nil {
				reason = outcome.err.Error()
			}
			releaseAttemptFailure(lease, 0, "", reason)
			excludeFailedAttempt(excludedAccounts, lease)
			if incoming.Context().Err() != nil {
				return
			}
			writeUncertainUpstreamFailure(writer, "anthropic", requestID, lastModelVersion, lastResponseID, reconnects, outcome.unsafeOutput, "上游流未正常完成："+reason)
			return
		}
	}
}

func numberAsInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

// streamOpenAIResponse streams an OpenAI SSE response converting to Gemini format.
func streamOpenAIResponse(w http.ResponseWriter, resp *http.Response, requestID string, attempt int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	streamState := openAIStreamState{traceID: requestID}
	startedAt := time.Now()
	var firstByteAt time.Time
	wroteEvent := false

	for scanner.Scan() {
		line := scanner.Text()
		if firstByteAt.IsZero() && len(line) > 0 {
			firstByteAt = time.Now()
		}
		geminiLine := convertOpenAILineToGemini(line, &streamState)
		if geminiLine != "" {
			if !wroteEvent {
				w.WriteHeader(http.StatusOK)
				wroteEvent = true
			}
			w.Write([]byte(geminiLine))
			if canFlush {
				flusher.Flush()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if !wroteEvent {
			writeEmptyUpstreamStreamError(w, "openai", requestID, attempt, resp.Header.Get("Content-Type"), err.Error())
			return
		}
		trace("openai-stream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error(), "downstreamCommitted": true})
	}
	if !wroteEvent {
		writeEmptyUpstreamStreamError(w, "openai", requestID, attempt, resp.Header.Get("Content-Type"), "上游响应中没有可识别的 OpenAI SSE 事件")
		return
	}
	if !streamState.finished {
		trace("openai-stream-missing-stop", map[string]any{"requestId": requestID, "attempt": attempt})
		w.Write([]byte(encodeAntigravityStreamEvent(finalStopResponse(streamState.modelVersion, streamState.responseID), requestID)))
		if canFlush {
			flusher.Flush()
		}
	}

	if streamState.usage != nil {
		promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens := openAIUsage(streamState.usage)
		trace("usage", map[string]any{
			"requestId":        requestID,
			"promptTokens":     promptTokens,
			"completionTokens": completionTokens,
			"cacheReadTokens":  cacheReadTokens,
			"cacheWriteTokens": cacheWriteTokens,
			"firstByteMs":      firstByteAt.Sub(startedAt).Milliseconds(),
			"totalMs":          time.Since(startedAt).Milliseconds(),
		})
	}
}

// streamOpenAIResponsesResponse converts Responses API SSE events into the
// internal Antigravity/Gemini envelope. It intentionally shares the same
// downstream headers and empty-stream diagnostics as Chat Completions.
func streamOpenAIResponsesResponse(w http.ResponseWriter, resp *http.Response, requestID string, attempt int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	state := openAIResponsesStreamState{traceID: requestID}
	startedAt := time.Now()
	var firstByteAt time.Time
	wroteEvent := false
	for scanner.Scan() {
		line := scanner.Text()
		if firstByteAt.IsZero() && line != "" {
			firstByteAt = time.Now()
		}
		geminiLine := convertOpenAIResponsesLineToGemini(line, &state)
		if geminiLine == "" {
			continue
		}
		if !wroteEvent {
			w.WriteHeader(http.StatusOK)
			wroteEvent = true
		}
		_, _ = w.Write([]byte(geminiLine))
		if canFlush {
			flusher.Flush()
		}
	}
	if err := scanner.Err(); err != nil {
		if !wroteEvent {
			writeEmptyUpstreamStreamError(w, "responses", requestID, attempt, resp.Header.Get("Content-Type"), err.Error())
			return
		}
		trace("responses-stream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error(), "downstreamCommitted": true})
	}
	if !wroteEvent {
		writeEmptyUpstreamStreamError(w, "responses", requestID, attempt, resp.Header.Get("Content-Type"), "上游响应中没有可识别的 Responses SSE 事件")
		return
	}
	if !state.finished {
		trace("responses-stream-missing-stop", map[string]any{"requestId": requestID, "attempt": attempt})
		_, _ = w.Write([]byte(responsesFinishEvent("STOP", &state)))
		if canFlush {
			flusher.Flush()
		}
	}
	if state.usage != nil {
		prompt, _ := numberAsInt(state.usage["input_tokens"])
		completion, _ := numberAsInt(state.usage["output_tokens"])
		trace("usage", map[string]any{
			"requestId": requestID, "promptTokens": prompt, "completionTokens": completion,
			"firstByteMs": firstByteAt.Sub(startedAt).Milliseconds(), "totalMs": time.Since(startedAt).Milliseconds(),
		})
	}
}

func openAIUsage(usage map[string]any) (prompt, completion, cacheRead, cacheWrite int) {
	prompt, _ = numberAsInt(usage["prompt_tokens"])
	completion, _ = numberAsInt(usage["completion_tokens"])
	if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		cacheRead, _ = numberAsInt(details["cached_tokens"])
		if value, ok := numberAsInt(details["cache_write_tokens"]); ok {
			cacheWrite = value
		}
	}
	if value, ok := numberAsInt(usage["cache_read_input_tokens"]); ok {
		cacheRead = value
	}
	if value, ok := numberAsInt(usage["cache_creation_input_tokens"]); ok {
		cacheWrite = value
	}
	return
}

// streamAnthropicResponse streams an Anthropic SSE response converting to Gemini format.
func streamAnthropicResponse(w http.ResponseWriter, resp *http.Response, requestID string, attempt int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	startedAt := time.Now()
	var firstByteAt time.Time
	totals := anthropicUsageTotals{}
	state := anthropicStreamState{traceID: requestID}
	wroteEvent := false

	for scanner.Scan() {
		line := scanner.Text()
		if firstByteAt.IsZero() && len(line) > 0 {
			firstByteAt = time.Now()
		}
		collectAnthropicUsage(line, &totals)
		geminiLine := convertAnthropicLineToGemini(line, &state)
		if geminiLine != "" {
			if !wroteEvent {
				w.WriteHeader(http.StatusOK)
				wroteEvent = true
			}
			w.Write([]byte(geminiLine))
			if canFlush {
				flusher.Flush()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if !wroteEvent {
			writeEmptyUpstreamStreamError(w, "anthropic", requestID, attempt, resp.Header.Get("Content-Type"), err.Error())
			return
		}
		trace("anthropic-stream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error(), "downstreamCommitted": true})
	}
	if !wroteEvent {
		writeEmptyUpstreamStreamError(w, "anthropic", requestID, attempt, resp.Header.Get("Content-Type"), "上游响应中没有可识别的 Anthropic SSE 事件")
		return
	}
	if !state.finished {
		trace("anthropic-stream-missing-stop", map[string]any{"requestId": requestID, "attempt": attempt})
		w.Write([]byte(encodeAntigravityStreamEvent(finalStopResponse(state.modelVersion, state.responseID), requestID)))
		if canFlush {
			flusher.Flush()
		}
	}

	if totals.seen {
		trace("usage", map[string]any{
			"requestId":        requestID,
			"promptTokens":     totals.input + totals.cacheRead + totals.cacheWrite,
			"completionTokens": totals.output,
			"cacheReadTokens":  totals.cacheRead,
			"cacheWriteTokens": totals.cacheWrite,
			"firstByteMs":      firstByteAt.Sub(startedAt).Milliseconds(),
			"totalMs":          time.Since(startedAt).Milliseconds(),
		})
	}
}

func finalStopResponse(modelVersion, responseID string) map[string]any {
	response := map[string]any{
		"candidates": []any{map[string]any{
			"content":      map[string]any{"role": "model", "parts": []any{}},
			"finishReason": "STOP",
		}},
	}
	if modelVersion != "" {
		response["modelVersion"] = modelVersion
	}
	if responseID != "" {
		response["responseId"] = responseID
	}
	return response
}

func writeEmptyUpstreamStreamError(w http.ResponseWriter, provider, requestID string, attempt int, contentType, message string) {
	trace(provider+"-empty-stream", map[string]any{
		"requestId":   requestID,
		"attempt":     attempt,
		"contentType": contentType,
		"message":     message,
	})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Del("Content-Disposition")
	w.Header().Del("Connection")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_upstream_stream",
		},
	})
}

func isRetryableStatus(code int) bool {
	return code == 429 || code == 502 || code == 503 || code == 504 || code == 524
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if seconds, err := strconv.ParseFloat(retryAfter, 64); err == nil && seconds >= 0 {
		return minDuration(time.Duration(seconds*float64(time.Second)), 10*time.Second)
	}
	if date, err := http.ParseTime(retryAfter); err == nil {
		return minDuration(maxDuration(time.Until(date), 0), 10*time.Second)
	}
	delay := 250 * time.Millisecond * time.Duration(1<<(attempt-1))
	return minDuration(delay, 2*time.Second)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// streamOpenAIAttempt converts one upstream stream but deliberately does not
// synthesize a stop event when the upstream connection vanishes. The caller
// can then keep the same downstream SSE response alive and retry safely.
func streamOpenAIAttempt(writer *downstreamSSEWriter, resp *http.Response, requestID string, attempt int, usageTrace *accountUsageTraceContext) streamAttemptOutcome {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	state := openAIStreamState{traceID: requestID}
	startedAt := time.Now()
	var firstByteAt time.Time
	outcome := streamAttemptOutcome{}

	for scanner.Scan() {
		line := scanner.Text()
		if firstByteAt.IsZero() && line != "" {
			firstByteAt = time.Now()
		}
		if event := convertOpenAILineToGemini(line, &state); event != "" {
			writer.write(event)
			outcome.wroteEvent = true
		}
	}
	if err := scanner.Err(); err != nil {
		outcome.err = err
		trace("openai-stream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error(), "downstreamCommitted": writer.committed})
	}
	if state.done && !state.finished {
		writer.write(encodeAntigravityStreamEvent(finalStopResponse(state.modelVersion, state.responseID), requestID))
		outcome.wroteEvent = true
	}
	outcome.finished = state.finished || state.done
	outcome.emittedText = state.emittedText.String()
	outcome.unsafeOutput = state.unsafeOutput
	outcome.upstreamStarted = state.upstreamStarted
	outcome.responseID = state.responseID
	outcome.modelVersion = state.modelVersion
	if state.usage != nil {
		promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens := openAIUsage(state.usage)
		traceAccountUsage(usageTrace, map[string]any{
			"requestId": requestID, "promptTokens": promptTokens, "completionTokens": completionTokens,
			"cacheReadTokens": cacheReadTokens, "cacheWriteTokens": cacheWriteTokens,
			"firstByteMs": firstByteAt.Sub(startedAt).Milliseconds(), "totalMs": time.Since(startedAt).Milliseconds(),
		})
	}
	return outcome
}

func streamOpenAIResponsesAttempt(writer *downstreamSSEWriter, resp *http.Response, requestID string, attempt int, usageTrace *accountUsageTraceContext) streamAttemptOutcome {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	state := openAIResponsesStreamState{traceID: requestID}
	startedAt := time.Now()
	var firstByteAt time.Time
	outcome := streamAttemptOutcome{}
	for scanner.Scan() {
		line := scanner.Text()
		if firstByteAt.IsZero() && line != "" {
			firstByteAt = time.Now()
		}
		if event := convertOpenAIResponsesLineToGemini(line, &state); event != "" {
			writer.write(event)
			outcome.wroteEvent = true
		}
	}
	if err := scanner.Err(); err != nil {
		outcome.err = err
		trace("responses-stream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error(), "downstreamCommitted": writer.committed})
	}
	if state.done && !state.finished {
		// A number of OpenAI-compatible Responses gateways terminate with the
		// generic [DONE] marker instead of response.completed. Convert it into
		// Antigravity's terminal event so the client never waits forever.
		writer.write(responsesFinishEvent("STOP", &state))
		outcome.wroteEvent = true
	}
	outcome.finished = state.finished || state.done
	outcome.emittedText = state.emittedText.String()
	outcome.unsafeOutput = state.unsafeOutput
	outcome.upstreamStarted = state.upstreamStarted
	outcome.responseID = state.responseID
	outcome.modelVersion = state.modelVersion
	if state.usage != nil {
		prompt, _ := numberAsInt(state.usage["input_tokens"])
		completion, _ := numberAsInt(state.usage["output_tokens"])
		traceAccountUsage(usageTrace, map[string]any{
			"requestId": requestID, "promptTokens": prompt, "completionTokens": completion,
			"firstByteMs": firstByteAt.Sub(startedAt).Milliseconds(), "totalMs": time.Since(startedAt).Milliseconds(),
		})
	}
	return outcome
}

func streamAnthropicAttempt(writer *downstreamSSEWriter, resp *http.Response, requestID string, attempt int, usageTrace *accountUsageTraceContext) streamAttemptOutcome {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	startedAt := time.Now()
	var firstByteAt time.Time
	totals := anthropicUsageTotals{}
	state := anthropicStreamState{traceID: requestID}
	outcome := streamAttemptOutcome{}

	for scanner.Scan() {
		line := scanner.Text()
		if firstByteAt.IsZero() && line != "" {
			firstByteAt = time.Now()
		}
		collectAnthropicUsage(line, &totals)
		if event := convertAnthropicLineToGemini(line, &state); event != "" {
			writer.write(event)
			outcome.wroteEvent = true
		}
	}
	if err := scanner.Err(); err != nil {
		outcome.err = err
		trace("anthropic-stream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error(), "downstreamCommitted": writer.committed})
	}
	outcome.finished = state.finished
	outcome.emittedText = state.emittedText.String()
	outcome.unsafeOutput = state.unsafeOutput
	outcome.upstreamStarted = state.upstreamStarted
	outcome.responseID = state.responseID
	outcome.modelVersion = state.modelVersion
	if totals.seen {
		traceAccountUsage(usageTrace, map[string]any{
			"requestId": requestID, "promptTokens": totals.input + totals.cacheRead + totals.cacheWrite,
			"completionTokens": totals.output, "cacheReadTokens": totals.cacheRead, "cacheWriteTokens": totals.cacheWrite,
			"firstByteMs": firstByteAt.Sub(startedAt).Milliseconds(), "totalMs": time.Since(startedAt).Milliseconds(),
		})
	}
	return outcome
}
