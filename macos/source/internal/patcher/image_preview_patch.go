package patcher

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// imagePreviewPatchMarker identifies the current renderer fallback that makes
// a generated-image tool result visible even when a newer Antigravity build
// stores it in generatedImage, base64Data, or a local file URI rather than
// the older generatedMedia URI shape.
//
// Keep this marker versioned. v3 was shipped by an earlier helper build, but
// its Windows file URI conversion leaves a leading slash before drive letters
// ("/C:/...") and therefore cannot render the affected Chinese-home-directory
// paths. v4 is the first complete, migratable version.
const imagePreviewPatchMarker = "antigravity-wf:image-preview-fallback:v4"

const imagePreviewPatchV3Marker = "antigravity-wf:image-preview-fallback:v3"

const imagePreviewPatchV2Marker = "antigravity-wf:image-preview-fallback:v2"

var imagePreviewRendererRelativePaths = []string{
	"out/main.js",
	"out/jetskiAgent/main.js",
	"out/vs/workbench/workbench.desktop.main.js",
}

// Packaged Agent builds do not consistently use the unpacked IDE paths. Keep
// the known renderer entries explicit rather than walking or rewriting
// arbitrary JavaScript in an ASAR archive.
var imagePreviewASARRelativePaths = []string{
	"dist/main.js",
	"dist/jetskiAgent/main.js",
	"dist/vs/workbench/workbench.desktop.main.js",
	"out/main.js",
	"out/jetskiAgent/main.js",
	"out/vs/workbench/workbench.desktop.main.js",
}

// This is the unmodified Antigravity 1.23.2 image-generation renderer shape.
// It is intentionally strict: a future renderer that does not match is left
// untouched, so an image-preview compatibility improvement can never block
// the normal local-proxy endpoint patch.
var imagePreviewOriginalRendererPattern = regexp.MustCompile(
	`([A-Za-z_$][A-Za-z0-9_$]*)=([A-Za-z_$][A-Za-z0-9_$]*)\.generatedMedia,([A-Za-z_$][A-Za-z0-9_$]*);` +
		`([A-Za-z_$][A-Za-z0-9_$]*)\?\.uri\?([A-Za-z_$][A-Za-z0-9_$]*)=([A-Za-z_$][A-Za-z0-9_$]*)\?\.\(([A-Za-z_$][A-Za-z0-9_$]*)\.uri\)\|\|void 0:` +
		`([A-Za-z_$][A-Za-z0-9_$]*)\?\.payload\.case==="inlineData"&&\(([A-Za-z_$][A-Za-z0-9_$]*)=([A-Za-z_$][A-Za-z0-9_$]*)\?([A-Za-z_$][A-Za-z0-9_$]*)\(([A-Za-z_$][A-Za-z0-9_$]*)\):void 0\);`,
)

// Legacy v2/v3 blocks were produced by this application, so they are allowed
// to be migrated only after their complete generated-image expression has been
// structurally recognised. Do not perform a broad marker-only substitution:
// a renderer could contain more than one matching expression and a marker for
// one must never hide another unfinished expression.
var imagePreviewLegacyHeaderPattern = regexp.MustCompile(
	`^/\*antigravity-wf:image-preview-fallback:(v2|v3)\*/` +
		`([A-Za-z_$][A-Za-z0-9_$]*)=([A-Za-z_$][A-Za-z0-9_$]*)\.generatedMedia\|\|([A-Za-z_$][A-Za-z0-9_$]*)\.generatedImage,([A-Za-z_$][A-Za-z0-9_$]*);`,
)

var imagePreviewLegacyResolverPattern = regexp.MustCompile(
	`([A-Za-z_$][A-Za-z0-9_$]*)\?\.uri\?\(([A-Za-z_$][A-Za-z0-9_$]*)=([A-Za-z_$][A-Za-z0-9_$]*)\?\.\(([A-Za-z_$][A-Za-z0-9_$]*)\.uri\),`,
)

var imagePreviewLegacyInlinePattern = regexp.MustCompile(
	`([A-Za-z_$][A-Za-z0-9_$]*)\?\.payload\?\.case==="inlineData"&&\(([A-Za-z_$][A-Za-z0-9_$]*)=([A-Za-z_$][A-Za-z0-9_$]*)\?([A-Za-z_$][A-Za-z0-9_$]*)\(([A-Za-z_$][A-Za-z0-9_$]*)\):void 0\)`,
)

var imagePreviewLegacyBlockEndPattern = regexp.MustCompile(`;let\s+`)

type imagePreviewPatchResult struct {
	Recognized bool
	Changed    bool
}

func patchImagePreviewRenderer(source string) (string, imagePreviewPatchResult) {
	result := imagePreviewPatchResult{Recognized: strings.Contains(source, imagePreviewPatchMarker)}

	var changed bool
	source, result.Recognized, changed = upgradeLegacyImagePreviewRenderers(source, result.Recognized)
	result.Changed = changed

	updated, recognized, changed := patchOriginalImagePreviewRenderers(source)
	source = updated
	result.Recognized = result.Recognized || recognized
	result.Changed = result.Changed || changed
	return source, result
}

func upgradeLegacyImagePreviewRenderers(source string, recognized bool) (string, bool, bool) {
	var output strings.Builder
	cursor, searchFrom := 0, 0
	changed := false
	for {
		start, marker := nextLegacyImagePreviewMarker(source, searchFrom)
		if start < 0 {
			break
		}
		endMatch := imagePreviewLegacyBlockEndPattern.FindStringIndex(source[start:])
		if endMatch == nil || endMatch[0] > 8*1024 {
			searchFrom = start + len(marker)
			continue
		}
		end := start + endMatch[0] // points at the old block's final semicolon.
		replacement, ok := upgradeLegacyImagePreviewBlock(source[start:end])
		if !ok {
			searchFrom = start + len(marker)
			continue
		}
		output.WriteString(source[cursor:start])
		output.WriteString(replacement)
		// replacement includes the semicolon. Skip the original semicolon and
		// preserve the following `let` declaration verbatim.
		cursor = end + 1
		searchFrom = cursor
		recognized = true
		changed = true
	}
	if !changed {
		return source, recognized, false
	}
	output.WriteString(source[cursor:])
	return output.String(), recognized, true
}

func nextLegacyImagePreviewMarker(source string, from int) (int, string) {
	v2 := strings.Index(source[from:], "/*"+imagePreviewPatchV2Marker+"*/")
	v3 := strings.Index(source[from:], "/*"+imagePreviewPatchV3Marker+"*/")
	if v2 < 0 && v3 < 0 {
		return -1, ""
	}
	if v2 >= 0 && (v3 < 0 || v2 < v3) {
		return from + v2, imagePreviewPatchV2Marker
	}
	return from + v3, imagePreviewPatchV3Marker
}

func upgradeLegacyImagePreviewBlock(block string) (string, bool) {
	header := imagePreviewLegacyHeaderPattern.FindStringSubmatch(block)
	if header == nil || header[3] != header[4] {
		return "", false
	}
	media, step, image := header[2], header[3], header[5]
	resolver := imagePreviewLegacyResolverPattern.FindStringSubmatch(block)
	inline := imagePreviewLegacyInlinePattern.FindStringSubmatch(block)
	if resolver == nil || inline == nil ||
		resolver[1] != media || resolver[2] != image || resolver[4] != media ||
		inline[1] != media || inline[2] != image || inline[3] != media ||
		!strings.Contains(block, "!"+image+"&&"+media+"?.base64Data") {
		return "", false
	}
	if header[1] == "v3" && (!strings.Contains(block, `startsWith("file://")`) || !strings.Contains(block, "decodeURIComponent")) {
		return "", false
	}
	return imagePreviewV4Renderer(media, step, image, resolver[3], inline[4]), true
}

func patchOriginalImagePreviewRenderers(source string) (string, bool, bool) {
	matches := imagePreviewOriginalRendererPattern.FindAllStringSubmatchIndex(source, -1)
	if len(matches) == 0 {
		return source, false, false
	}
	var output strings.Builder
	last := 0
	recognized, changed := false, false
	for _, match := range matches {
		media := source[match[2]:match[3]]
		step := source[match[4]:match[5]]
		image := source[match[6]:match[7]]
		mediaURI := source[match[8]:match[9]]
		imageURI := source[match[10]:match[11]]
		resolver := source[match[12]:match[13]]
		mediaURIArgument := source[match[14]:match[15]]
		mediaPayload := source[match[16]:match[17]]
		imageInline := source[match[18]:match[19]]
		mediaInline := source[match[20]:match[21]]
		inlineData := source[match[22]:match[23]]
		mediaInlineArgument := source[match[24]:match[25]]
		if media != mediaURI || media != mediaURIArgument || media != mediaPayload || media != mediaInline || media != mediaInlineArgument || image != imageURI || image != imageInline {
			continue
		}
		recognized = true
		changed = true
		output.WriteString(source[last:match[0]])
		output.WriteString(imagePreviewV4Renderer(media, step, image, resolver, inlineData))
		last = match[1]
	}
	if !changed {
		return source, false, false
	}
	output.WriteString(source[last:])
	return output.String(), recognized, true
}

func imagePreviewV4Renderer(media, step, image, resolver, inlineData string) string {
	return "/*" + imagePreviewPatchMarker + "*/" + media + "=" + step + ".generatedMedia||" + step + ".generatedImage," + image + ";" +
		imagePreviewResolveMedia(media, image, resolver, inlineData) +
		",!" + image + "&&" + step + ".generatedImage&&" + step + ".generatedImage!==" + media + "&&(" +
		media + "=" + step + ".generatedImage," + image + "=void 0," + imagePreviewResolveMedia(media, image, resolver, inlineData) + ");"
}

// imagePreviewResolveMedia emits a comma-expression that assigns image only
// when a source is actually available. imagePreviewV4Renderer calls it first
// for generatedMedia and then, if necessary, for generatedImage; a metadata
// object in generatedMedia can no longer hide a usable generatedImage URI.
func imagePreviewResolveMedia(media, image, resolver, inlineData string) string {
	return media + "?.uri?(" + image + "=" + resolver + "?.(" + media + ".uri)," + image + "=(" + image + "&&typeof " + image + ".getState===\"function\"?" + image + ".getState():" + image + "||void 0)):" +
		media + "?.payload?.case===\"inlineData\"&&(" + image + "=" + media + "?" + inlineData + "(" + media + "):void 0),!" + image + "&&" + media + "?.base64Data&&(" + image + "=\"data:\"+(" + media + ".mimeType||\"image/png\")+\";base64,\"+(typeof " + media + ".base64Data===\"string\"?" + media + ".base64Data:btoa(Array.from(" + media + ".base64Data).map(i=>String.fromCharCode(i)).join(\"\"))))" +
		imagePreviewFileURIFallback(media, image)
}

func imagePreviewFileURIFallback(media, image string) string {
	// The IIFE keeps the fallback's local names out of the minified renderer
	// scope. It intentionally catches malformed percent encodings: a bad file
	// name must not throw from React rendering and blank the whole chat step.
	// file://server/share is retained as //server/share for Windows UNC paths.
	return ",!" + image + "&&" + media + "?.uri&&typeof " + media + ".uri===\"string\"&&" + media + ".uri.startsWith(\"file://\")&&(" + image + "=((u)=>{let p=u.replace(/^file:\\/\\/\\/([A-Za-z]:\\/)/,\"$1\");p===u&&(p=u.startsWith(\"file:///\")?u.slice(7):u.replace(/^file:\\/\\//,\"//\"));try{return decodeURIComponent(p)}catch{return p}})(" + media + ".uri))"
}

func imagePreviewRendererPaths(root string) []string {
	var paths []string
	for _, relative := range imagePreviewRendererRelativePaths {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			paths = append(paths, path)
		}
	}
	return paths
}

// imagePreviewRenderersNeedPatch reports only known, safely patchable 1.23.2
// renderer shapes that are still missing the current fallback. Missing files,
// unreadable optional bundles, and future renderer shapes intentionally do not
// count as pending: they must never make an otherwise valid endpoint patch look
// inactive.
func imagePreviewRenderersNeedPatch(paths []string) bool {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if _, result := patchImagePreviewRenderer(string(data)); result.Changed {
			return true
		}
	}
	return false
}

func imagePreviewASARRendererEntries(archive *asarArchive) []string {
	var entries []string
	for _, relative := range imagePreviewASARRelativePaths {
		node, err := archive.node(relative)
		if err != nil || node.Files != nil || node.Unpacked || node.Size == nil {
			continue
		}
		entries = append(entries, relative)
	}
	sort.Strings(entries)
	return entries
}

// imagePreviewASARUnpackedRendererPaths resolves only declared unpacked
// renderer entries. They must be patched beside app.asar, never added to the
// archive replacement map (which would silently change their ASAR semantics).
func imagePreviewASARUnpackedRendererPaths(archive *asarArchive) []string {
	var paths []string
	for _, relative := range imagePreviewASARRelativePaths {
		node, err := archive.node(relative)
		if err != nil || node.Files != nil || !node.Unpacked {
			continue
		}
		path := filepath.Join(archive.path+".unpacked", filepath.FromSlash(relative))
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func imagePreviewASARUnpackedRendererPathsForPath(path string) []string {
	archive, err := readASAR(path)
	if err != nil {
		return nil
	}
	return imagePreviewASARUnpackedRendererPaths(archive)
}

func patchImagePreviewASARRenderers(archive *asarArchive, replacements map[string][]byte) bool {
	changed := false
	for _, entry := range imagePreviewASARRendererEntries(archive) {
		data, ok := replacements[entry]
		if !ok {
			var err error
			data, err = archive.readFile(entry)
			if err != nil {
				// This optional compatibility patch must never make a normal
				// endpoint patch fail merely because an unfamiliar renderer is
				// unreadable or absent.
				continue
			}
		}
		updated, result := patchImagePreviewRenderer(string(data))
		if result.Changed {
			replacements[entry] = []byte(updated)
			changed = true
		}
	}
	return changed
}

func imagePreviewASARArchiveNeedsPatch(path string) bool {
	archive, err := readASAR(path)
	if err != nil {
		return false
	}
	for _, entry := range imagePreviewASARRendererEntries(archive) {
		data, err := archive.readFile(entry)
		if err != nil {
			continue
		}
		if _, result := patchImagePreviewRenderer(string(data)); result.Changed {
			return true
		}
	}
	return false
}

func imagePreviewASARNeedsPatch(path string) bool {
	archive, err := readASAR(path)
	if err != nil {
		return false
	}
	for _, entry := range imagePreviewASARRendererEntries(archive) {
		data, err := archive.readFile(entry)
		if err != nil {
			continue
		}
		if _, result := patchImagePreviewRenderer(string(data)); result.Changed {
			return true
		}
	}
	return imagePreviewRenderersNeedPatch(imagePreviewASARUnpackedRendererPaths(archive))
}
