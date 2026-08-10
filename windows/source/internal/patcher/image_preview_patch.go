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
// paths. v6 additionally maps local file URIs to the VS Code browser-resource
// scheme allowed by the workbench CSP. v7 also corrects the matching native
// image-tool UI: it exposes the actual image model and opens the image result
// by default without changing the upstream media payload. v8 makes the
// in-progress title neutral until a real model is present and preserves
// generic Gemini model names such as gemini-3.6-flash.
const imagePreviewPatchMarker = "antigravity-wf:image-preview-fallback:v8"

const imagePreviewPatchV7Marker = "antigravity-wf:image-preview-fallback:v7"

const imagePreviewPatchV6Marker = "antigravity-wf:image-preview-fallback:v6"

const imageGenerationUIPatchMarker = "antigravity-wf:image-generation-ui:v2"

const imageGenerationUIPatchV1Marker = "antigravity-wf:image-generation-ui:v1"

const imageGenerationDedupePatchMarker = "antigravity-wf:image-generation-dedupe:v1"

const imagePreviewPatchV5Marker = "antigravity-wf:image-preview-fallback:v5"

const imagePreviewPatchV4Marker = "antigravity-wf:image-preview-fallback:v4"

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
	`^/\*antigravity-wf:image-preview-fallback:(v2|v3|v4|v5|v6|v7)\*/` +
		`([A-Za-z_$][A-Za-z0-9_$]*)=([A-Za-z_$][A-Za-z0-9_$]*)\.generatedMedia\|\|([A-Za-z_$][A-Za-z0-9_$]*)\.generatedImage,([A-Za-z_$][A-Za-z0-9_$]*);`,
)

var imagePreviewLegacyResolverPattern = regexp.MustCompile(
	`([A-Za-z_$][A-Za-z0-9_$]*)\?\.uri\?\(([A-Za-z_$][A-Za-z0-9_$]*)=([A-Za-z_$][A-Za-z0-9_$]*)\?\.\(([A-Za-z_$][A-Za-z0-9_$]*)\.uri\),`,
)

var imagePreviewLegacyInlinePattern = regexp.MustCompile(
	`([A-Za-z_$][A-Za-z0-9_$]*)\?\.payload\?\.case==="inlineData"&&\(([A-Za-z_$][A-Za-z0-9_$]*)=([A-Za-z_$][A-Za-z0-9_$]*)\?([A-Za-z_$][A-Za-z0-9_$]*)\(([A-Za-z_$][A-Za-z0-9_$]*)\):void 0\)`,
)

var imagePreviewLegacyBlockEndPattern = regexp.MustCompile(`;let\s+`)

var imagePreviewCurrentHeaderPattern = regexp.MustCompile(
	`^/\*` + regexp.QuoteMeta(imagePreviewPatchMarker) + `\*/` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\.generatedMedia\|\|(` + imagePreviewJavaScriptIdentifier + `)\.generatedImage,(` + imagePreviewJavaScriptIdentifier + `);`,
)

const imagePreviewJavaScriptIdentifier = `[A-Za-z_$][A-Za-z0-9_$]*`

// These two patterns match the complete native image-tool title and result
// components observed in Antigravity 1.23.2. They deliberately include the
// surrounding JSX shape so a similarly named string elsewhere in a future
// renderer cannot be changed accidentally.
var imageGenerationTitleRendererPattern = regexp.MustCompile(
	`(` + imagePreviewJavaScriptIdentifier + `)=\(\{step:(` + imagePreviewJavaScriptIdentifier + `),status:(` + imagePreviewJavaScriptIdentifier + `)\}\)=>\{let ` +
		`(` + imagePreviewJavaScriptIdentifier + `)=!!(` + imagePreviewJavaScriptIdentifier + `)\.generatedImage\?\.uri,` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\.modelName\?(` + imagePreviewJavaScriptIdentifier + `)\[(` + imagePreviewJavaScriptIdentifier + `)\.modelName\]:void 0,` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\?\.displayName\|\|"Gemini",` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\?\.isNewModel\?\?!1;return `,
)

var imageGenerationResultRendererPattern = regexp.MustCompile(
	`(` + imagePreviewJavaScriptIdentifier + `)=\(\{step:(` + imagePreviewJavaScriptIdentifier + `),status:(` + imagePreviewJavaScriptIdentifier + `),error:(` + imagePreviewJavaScriptIdentifier + `)\}\)=>` +
		`(` + imagePreviewJavaScriptIdentifier + `)\((` + imagePreviewJavaScriptIdentifier + `),\{loading:(` + imagePreviewJavaScriptIdentifier + `)\((` + imagePreviewJavaScriptIdentifier + `)\),title:(` + imagePreviewJavaScriptIdentifier + `)\((` + imagePreviewJavaScriptIdentifier + `),\{step:(` + imagePreviewJavaScriptIdentifier + `),status:(` + imagePreviewJavaScriptIdentifier + `)\}\),supplementaryView:(` + imagePreviewJavaScriptIdentifier + `)\?null:(` + imagePreviewJavaScriptIdentifier + `)\((` + imagePreviewJavaScriptIdentifier + `),\{step:(` + imagePreviewJavaScriptIdentifier + `),status:(` + imagePreviewJavaScriptIdentifier + `)\}\),cta:null\}\)`,
)

var imageGenerationExpansionHooksPattern = regexp.MustCompile(
	`\[(` + imagePreviewJavaScriptIdentifier + `),(` + imagePreviewJavaScriptIdentifier + `)\]=(` + imagePreviewJavaScriptIdentifier + `)\(!1\),` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\(\(\)=>\{(` + imagePreviewJavaScriptIdentifier + `)\((` + imagePreviewJavaScriptIdentifier + `)=>!(` + imagePreviewJavaScriptIdentifier + `)\)\},\[\]\)`,
)

// Antigravity IDE 2.1.x combines the image title and result container into a
// single component. Keep the match anchored to the complete model-resolution
// prefix; a plain "Generated with" string is never sufficient for a rewrite.
var imageGenerationCombinedRendererPrefixPattern = regexp.MustCompile(
	`(` + imagePreviewJavaScriptIdentifier + `)=\(\{step:(` + imagePreviewJavaScriptIdentifier + `),status:(` + imagePreviewJavaScriptIdentifier + `),error:(` + imagePreviewJavaScriptIdentifier + `)\}\)=>\{let ` +
		`(` + imagePreviewJavaScriptIdentifier + `)=!!(` + imagePreviewJavaScriptIdentifier + `)\.generatedMedia\?\.uri,` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\.modelName\?(` + imagePreviewJavaScriptIdentifier + `)\[(` + imagePreviewJavaScriptIdentifier + `)\.modelName\]:void 0,` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\?\.displayName\|\|"Gemini",` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\?\.isNewModel\?\?!1,` +
		`(` + imagePreviewJavaScriptIdentifier + `)=`,
)

// This matches the title component emitted by the v1 UI patch, not arbitrary
// workbench code. The surrounding marker is checked separately before a
// migration is allowed.
var imageGenerationLegacyUITitleRendererPattern = regexp.MustCompile(
	`(` + imagePreviewJavaScriptIdentifier + `)=\(\{step:(` + imagePreviewJavaScriptIdentifier + `),status:(` + imagePreviewJavaScriptIdentifier + `)\}\)=>\{let ` +
		`(` + imagePreviewJavaScriptIdentifier + `)=!!(` + imagePreviewJavaScriptIdentifier + `)\.generatedImage\?\.uri,` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\.modelName\?(` + imagePreviewJavaScriptIdentifier + `)\[(` + imagePreviewJavaScriptIdentifier + `)\.modelName\]:void 0,` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\?\.displayName\|\|\((` + imagePreviewJavaScriptIdentifier + `)\.modelName\?\.replace\(/\^gpt-image-\(\\d\+\)\$/i,"GPT Image \$1"\)\)\|\|(` + imagePreviewJavaScriptIdentifier + `)\.modelName\|\|"Image generator",` +
		`(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\?\.isNewModel\?\?!1;return `,
)

var imageGenerationTitleChildrenPattern = regexp.MustCompile(
	`(` + imagePreviewJavaScriptIdentifier + `)\("span",\{children:(` + imagePreviewJavaScriptIdentifier + `)\((` + imagePreviewJavaScriptIdentifier + `)\)\?`,
)

// This is the dedicated Markdown artifact-image component used by the IDE.
// It is paired with the native generated-image preview before any rewrite is
// allowed, so normal Markdown images remain untouched.
var imageArtifactMarkdownRendererPrefixPattern = regexp.MustCompile(
	`(` + imagePreviewJavaScriptIdentifier + `)=\(\{src:(` + imagePreviewJavaScriptIdentifier + `),alt:(` + imagePreviewJavaScriptIdentifier + `),originalFilePath:(` + imagePreviewJavaScriptIdentifier + `),popout:(` + imagePreviewJavaScriptIdentifier + `)=!0,className:(` + imagePreviewJavaScriptIdentifier + `)="",openUri:(` + imagePreviewJavaScriptIdentifier + `)\}\)=>\{let\[(` + imagePreviewJavaScriptIdentifier + `),(` + imagePreviewJavaScriptIdentifier + `)\]=(` + imagePreviewJavaScriptIdentifier + `)\(!1\),(` + imagePreviewJavaScriptIdentifier + `)=(` + imagePreviewJavaScriptIdentifier + `)\((` + imagePreviewJavaScriptIdentifier + `)\),`,
)

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

	updated, recognized, changed = patchImageGenerationUIRenderers(source)
	source = updated
	result.Recognized = result.Recognized || recognized
	result.Changed = result.Changed || changed

	updated, recognized, changed = patchDuplicateGeneratedImageRenderers(source)
	source = updated
	result.Recognized = result.Recognized || recognized
	result.Changed = result.Changed || changed
	return source, result
}

func patchDuplicateGeneratedImageRenderers(source string) (string, bool, bool) {
	if strings.Contains(source, imageGenerationDedupePatchMarker) {
		return source, true, false
	}
	registrations := generatedImageRegistrationReplacements(source)
	markdown := imageArtifactMarkdownReplacement(source)
	if len(registrations) == 0 || markdown == nil {
		return source, false, false
	}
	replacements := append(registrations, *markdown)
	sort.Slice(replacements, func(left, right int) bool { return replacements[left].start < replacements[right].start })
	var output strings.Builder
	last := 0
	for _, replacement := range replacements {
		if replacement.start < last {
			return source, false, false
		}
		output.WriteString(source[last:replacement.start])
		output.WriteString(replacement.value)
		last = replacement.end
	}
	output.WriteString(source[last:])
	return output.String(), true, true
}

func generatedImageRegistrationReplacements(source string) []imagePreviewRendererReplacement {
	marker := "/*" + imagePreviewPatchMarker + "*/"
	var replacements []imagePreviewRendererReplacement
	searchFrom := 0
	for {
		startOffset := strings.Index(source[searchFrom:], marker)
		if startOffset < 0 {
			break
		}
		start := searchFrom + startOffset
		endMatch := imagePreviewLegacyBlockEndPattern.FindStringIndex(source[start:])
		if endMatch == nil || endMatch[0] > 8*1024 {
			searchFrom = start + len(marker)
			continue
		}
		end := start + endMatch[0] + 1
		block := source[start:end]
		header := imagePreviewCurrentHeaderPattern.FindStringSubmatch(block)
		if header == nil || header[2] != header[3] || strings.Contains(block, "__antigravityWFGeneratedImages") {
			searchFrom = start + len(marker)
			continue
		}
		media, image := header[1], header[4]
		if !strings.Contains(block, image+`=typeof `+image+`==="string"?`+image+`:void 0`) ||
			!strings.Contains(block, media+`?.uri`) {
			searchFrom = start + len(marker)
			continue
		}
		registration := `globalThis.__antigravityWFImageKey??=(value=>{let text=typeof value==="string"?value:"";if(!text)return"";try{text=decodeURIComponent(text)}catch{}text=text.replace(/^vscode-file:\/\/(?:vscode-app)?/i,"").replace(/^file:\/\//i,"").replace(/\\/g,"/").toLowerCase();return text.length>512?text.length+":"+text.slice(0,192)+":"+text.slice(-192):text}),globalThis.__antigravityWFGeneratedImages??=new Set,` +
			image + `&&globalThis.__antigravityWFGeneratedImages.add(globalThis.__antigravityWFImageKey(` + image + `)),` +
			media + `?.uri&&globalThis.__antigravityWFGeneratedImages.add(globalThis.__antigravityWFImageKey(` + media + `.uri));`
		replacements = append(replacements, imagePreviewRendererReplacement{start: end, end: end, value: registration})
		searchFrom = end
	}
	return replacements
}

func imageArtifactMarkdownReplacement(source string) *imagePreviewRendererReplacement {
	matches := imageArtifactMarkdownRendererPrefixPattern.FindAllStringSubmatchIndex(source, -1)
	if len(matches) != 1 {
		return nil
	}
	match := matches[0]
	sourceValue := imagePreviewSubmatch(source, match, 2)
	originalPath := imagePreviewSubmatch(source, match, 4)
	errorState := imagePreviewSubmatch(source, match, 8)
	resolvedValue := imagePreviewSubmatch(source, match, 11)
	if sourceValue != imagePreviewSubmatch(source, match, 13) {
		return nil
	}
	searchEnd := match[1] + 4*1024
	if searchEnd > len(source) {
		searchEnd = len(source)
	}
	segment := source[match[1]:searchEnd]
	returnNeedle := `;return!` + sourceValue + `||` + errorState + `?`
	returnOffset := strings.Index(segment, returnNeedle)
	if returnOffset < 0 || strings.Count(segment[:returnOffset+len(returnNeedle)], returnNeedle) != 1 {
		return nil
	}
	prefix := source[match[0]:match[1]]
	duplicate := `$wfImageDuplicate=!!globalThis.__antigravityWFImageKey&&!!globalThis.__antigravityWFGeneratedImages&&[` + sourceValue + `,` + resolvedValue + `,` + originalPath + `].some(value=>globalThis.__antigravityWFGeneratedImages.has(globalThis.__antigravityWFImageKey(value))),`
	prefix += duplicate
	beforeReturn := segment[:returnOffset]
	afterReturn := segment[returnOffset:]
	afterReturn = strings.Replace(afterReturn, returnNeedle, `;return $wfImageDuplicate?null:!`+sourceValue+`||`+errorState+`?`, 1)
	end := match[1] + len(segment)
	return &imagePreviewRendererReplacement{
		start: match[0], end: end,
		value: "/*" + imageGenerationDedupePatchMarker + "*/" + prefix + beforeReturn + afterReturn,
	}
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
	bestStart, bestMarker := -1, ""
	for _, marker := range []string{imagePreviewPatchV2Marker, imagePreviewPatchV3Marker, imagePreviewPatchV4Marker, imagePreviewPatchV5Marker, imagePreviewPatchV6Marker, imagePreviewPatchV7Marker} {
		index := strings.Index(source[from:], "/*"+marker+"*/")
		if index >= 0 && (bestStart < 0 || index < bestStart) {
			bestStart, bestMarker = index, marker
		}
	}
	if bestStart < 0 {
		return -1, ""
	}
	return from + bestStart, bestMarker
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
	if (header[1] == "v3" || header[1] == "v4" || header[1] == "v5") && (!strings.Contains(block, `startsWith("file://")`) || !strings.Contains(block, "decodeURIComponent")) {
		return "", false
	}
	if (header[1] == "v5" || header[1] == "v6" || header[1] == "v7") && !strings.Contains(block, `typeof `+image+`==="string"`) {
		return "", false
	}
	return imagePreviewV4Renderer(media, step, image, resolver[3], inline[4]), true
}

type imagePreviewRendererReplacement struct {
	start int
	end   int
	value string
}

type imageGenerationTitleRendererMatch struct {
	match []int
	end   int
}

func patchImageGenerationUIRenderers(source string) (string, bool, bool) {
	if strings.Contains(source, imageGenerationUIPatchMarker) {
		return source, true, false
	}
	if strings.Contains(source, imageGenerationUIPatchV1Marker) {
		return upgradeLegacyImageGenerationUIRenderers(source)
	}
	if updated, recognized, changed := patchCombinedImageGenerationUIRenderers(source); recognized || changed {
		return updated, recognized, changed
	}
	titleMatches := imageGenerationTitleRendererPattern.FindAllStringSubmatchIndex(source, -1)
	resultMatches := imageGenerationResultRendererPattern.FindAllStringSubmatchIndex(source, -1)
	if len(titleMatches) == 0 || len(resultMatches) == 0 {
		return source, false, false
	}

	titlesByComponent := make(map[string][]imageGenerationTitleRendererMatch, len(titleMatches))
	for _, titleMatch := range titleMatches {
		end, ok := imageGenerationTitleRendererEnd(source, titleMatch)
		if !ok {
			continue
		}
		component := imagePreviewSubmatch(source, titleMatch, 1)
		titlesByComponent[component] = append(titlesByComponent[component], imageGenerationTitleRendererMatch{match: titleMatch, end: end})
	}

	replacements := make([]imagePreviewRendererReplacement, 0, len(resultMatches)*2)
	usedTitleOffsets := map[int]bool{}
	for _, resultMatch := range resultMatches {
		if !validImageGenerationResultRendererMatch(source, resultMatch) {
			continue
		}
		titleComponent := imagePreviewSubmatch(source, resultMatch, 10)
		var titleMatch imageGenerationTitleRendererMatch
		for _, candidate := range titlesByComponent[titleComponent] {
			if candidate.end <= resultMatch[0] && resultMatch[0]-candidate.end <= 8*1024 && !usedTitleOffsets[candidate.match[0]] {
				titleMatch = candidate
				break
			}
		}
		if titleMatch.match == nil {
			continue
		}

		hookSearchEnd := resultMatch[1] + 4*1024
		if hookSearchEnd > len(source) {
			hookSearchEnd = len(source)
		}
		hookMatch := imageGenerationExpansionHooksPattern.FindStringSubmatchIndex(source[resultMatch[1]:hookSearchEnd])
		if hookMatch == nil || !validImageGenerationExpansionHooks(source[resultMatch[1]:hookSearchEnd], hookMatch) {
			continue
		}
		titleReplacement, ok := imageGenerationTitleRendererReplacement(source, titleMatch.match, titleMatch.end)
		if !ok {
			continue
		}
		resultReplacement := imageGenerationResultRendererReplacement(source, resultMatch, source[resultMatch[1]:hookSearchEnd], hookMatch)
		replacements = append(replacements,
			imagePreviewRendererReplacement{start: titleMatch.match[0], end: titleMatch.end, value: titleReplacement},
			imagePreviewRendererReplacement{start: resultMatch[0], end: resultMatch[1], value: resultReplacement},
		)
		usedTitleOffsets[titleMatch.match[0]] = true
	}
	if len(replacements) == 0 {
		return source, false, false
	}

	sort.Slice(replacements, func(left, right int) bool {
		return replacements[left].start < replacements[right].start
	})
	var output strings.Builder
	last := 0
	for _, replacement := range replacements {
		if replacement.start < last {
			return source, false, false
		}
		output.WriteString(source[last:replacement.start])
		output.WriteString(replacement.value)
		last = replacement.end
	}
	output.WriteString(source[last:])
	return output.String(), true, true
}

func patchCombinedImageGenerationUIRenderers(source string) (string, bool, bool) {
	matches := imageGenerationCombinedRendererPrefixPattern.FindAllStringSubmatchIndex(source, -1)
	if len(matches) == 0 {
		return source, false, false
	}
	replacements := make([]imagePreviewRendererReplacement, 0, len(matches))
	for _, match := range matches {
		step := imagePreviewSubmatch(source, match, 2)
		status := imagePreviewSubmatch(source, match, 3)
		hasMedia := imagePreviewSubmatch(source, match, 5)
		resolvedModel := imagePreviewSubmatch(source, match, 7)
		displayName := imagePreviewSubmatch(source, match, 11)
		isNewModel := imagePreviewSubmatch(source, match, 13)
		title := imagePreviewSubmatch(source, match, 15)
		if !sameImagePreviewIdentifiers(step,
			imagePreviewSubmatch(source, match, 6),
			imagePreviewSubmatch(source, match, 8),
			imagePreviewSubmatch(source, match, 10),
		) || !sameImagePreviewIdentifiers(resolvedModel,
			imagePreviewSubmatch(source, match, 12),
			imagePreviewSubmatch(source, match, 14),
		) {
			continue
		}
		endOffset := strings.Index(source[match[1]:], ";return ")
		if endOffset < 0 || endOffset > 2*1024 {
			continue
		}
		end := match[1] + endOffset
		current := source[match[0]:end]
		oldModelLabel := displayName + `=` + resolvedModel + `?.displayName||"Gemini"`
		if strings.Count(current, oldModelLabel) != 1 {
			continue
		}
		updated := strings.Replace(current, oldModelLabel, imageGenerationModelLabel(step, resolvedModel, displayName), 1)
		oldIsNewModel := `,` + isNewModel + `=` + resolvedModel + `?.isNewModel??!1,` + title + `=`
		newIsNewModel := `,` + isNewModel + `=` + resolvedModel + `?.isNewModel??!1,$wfIsGeminiImage=!!` + resolvedModel + `||/^gemini[-_]/i.test(` + step + `.modelName||""),` + title + `=`
		if strings.Count(updated, oldIsNewModel) != 1 {
			continue
		}
		updated = strings.Replace(updated, oldIsNewModel, newIsNewModel, 1)
		if strings.Count(updated, ":"+hasMedia+"?`Generated with ") != 1 {
			continue
		}
		for _, prefix := range []string{"Generating with ", "Generated with ", "Generate with "} {
			oldTitle := "`" + prefix + "${" + displayName + "} \\u{1F34C}`"
			newTitle := "`" + prefix + "${" + displayName + "}${$wfIsGeminiImage?\" \\u{1F34C}\":\"\"}`"
			if strings.Count(updated, oldTitle) != 1 {
				updated = ""
				break
			}
			updated = strings.Replace(updated, oldTitle, newTitle, 1)
		}
		if updated == "" {
			continue
		}
		titleAssignment := title + `=`
		titleOffset := strings.LastIndex(updated, titleAssignment)
		if titleOffset < 0 {
			continue
		}
		titleExpression := updated[titleOffset+len(titleAssignment):]
		loadingPattern := regexp.MustCompile(`^(` + imagePreviewJavaScriptIdentifier + `)\(` + regexp.QuoteMeta(status) + `\)\?`)
		loadingMatch := loadingPattern.FindStringSubmatch(titleExpression)
		if loadingMatch == nil {
			continue
		}
		loading := loadingMatch[1]
		neutralTitle := loading + `(` + status + `)&&!` + step + `.modelName?` + "`Generating image`:" + titleExpression
		updated = updated[:titleOffset+len(titleAssignment)] + neutralTitle
		replacements = append(replacements, imagePreviewRendererReplacement{
			start: match[0], end: end, value: "/*" + imageGenerationUIPatchMarker + "*/" + updated,
		})
	}
	if len(replacements) == 0 {
		return source, false, false
	}
	sort.Slice(replacements, func(left, right int) bool { return replacements[left].start < replacements[right].start })
	var output strings.Builder
	last := 0
	for _, replacement := range replacements {
		if replacement.start < last {
			return source, false, false
		}
		output.WriteString(source[last:replacement.start])
		output.WriteString(replacement.value)
		last = replacement.end
	}
	output.WriteString(source[last:])
	return output.String(), true, true
}

func imagePreviewSubmatch(source string, match []int, group int) string {
	start, end := match[group*2], match[group*2+1]
	if start < 0 || end < start {
		return ""
	}
	return source[start:end]
}

func imageGenerationTitleRendererEnd(source string, match []int) (int, bool) {
	if !sameImagePreviewIdentifiers(
		imagePreviewSubmatch(source, match, 2),
		imagePreviewSubmatch(source, match, 5),
		imagePreviewSubmatch(source, match, 7),
		imagePreviewSubmatch(source, match, 9),
	) || imagePreviewSubmatch(source, match, 6) != imagePreviewSubmatch(source, match, 13) {
		return 0, false
	}
	componentEndOffset := strings.Index(source[match[1]:], "})},")
	if componentEndOffset < 0 || componentEndOffset > 4*1024 {
		return 0, false
	}
	return match[1] + componentEndOffset + len("})}"), true
}

func validImageGenerationResultRendererMatch(source string, match []int) bool {
	return sameImagePreviewIdentifiers(
		imagePreviewSubmatch(source, match, 2),
		imagePreviewSubmatch(source, match, 11),
		imagePreviewSubmatch(source, match, 16),
	) && sameImagePreviewIdentifiers(
		imagePreviewSubmatch(source, match, 3),
		imagePreviewSubmatch(source, match, 8),
		imagePreviewSubmatch(source, match, 12),
		imagePreviewSubmatch(source, match, 17),
	) && sameImagePreviewIdentifiers(
		imagePreviewSubmatch(source, match, 4),
		imagePreviewSubmatch(source, match, 13),
	) && sameImagePreviewIdentifiers(
		imagePreviewSubmatch(source, match, 5),
		imagePreviewSubmatch(source, match, 9),
		imagePreviewSubmatch(source, match, 14),
	)
}

func validImageGenerationExpansionHooks(source string, match []int) bool {
	return imagePreviewSubmatch(source, match, 2) == imagePreviewSubmatch(source, match, 6) &&
		imagePreviewSubmatch(source, match, 7) == imagePreviewSubmatch(source, match, 8)
}

func sameImagePreviewIdentifiers(values ...string) bool {
	if len(values) == 0 || values[0] == "" {
		return false
	}
	for _, value := range values[1:] {
		if value != values[0] {
			return false
		}
	}
	return true
}

func imageGenerationTitleRendererReplacement(source string, match []int, end int) (string, bool) {
	current := source[match[0]:end]
	step := imagePreviewSubmatch(source, match, 2)
	resolvedModel := imagePreviewSubmatch(source, match, 6)
	displayName := imagePreviewSubmatch(source, match, 10)
	if step == "" || resolvedModel == "" || displayName == "" {
		return "", false
	}

	oldModelLabel := displayName + `=` + resolvedModel + `?.displayName||"Gemini"`
	newModelLabel := imageGenerationModelLabel(step, resolvedModel, displayName)
	if strings.Count(current, oldModelLabel) != 1 {
		return "", false
	}
	updated := strings.Replace(current, oldModelLabel, newModelLabel, 1)
	oldIsNewModel := `,` + imagePreviewSubmatch(source, match, 12) + `=` + resolvedModel + `?.isNewModel??!1;return `
	newIsNewModel := `,` + imagePreviewSubmatch(source, match, 12) + `=` + resolvedModel + `?.isNewModel??!1,$wfIsGeminiImage=!!` + resolvedModel + `||/^gemini[-_]/i.test(` + step + `.modelName||"");return `
	if strings.Count(updated, oldIsNewModel) != 1 {
		return "", false
	}
	updated = strings.Replace(updated, oldIsNewModel, newIsNewModel, 1)
	for _, prefix := range []string{"Generating with ", "Generated with ", "Generate with "} {
		oldTitle := "`" + prefix + "${" + displayName + "} \\u{1F34C}`"
		newTitle := "`" + prefix + "${" + displayName + "}${$wfIsGeminiImage?\" \\u{1F34C}\":\"\"}`"
		if strings.Count(updated, oldTitle) != 1 {
			return "", false
		}
		updated = strings.Replace(updated, oldTitle, newTitle, 1)
	}
	return addNeutralImageGenerationLoadingTitle(updated, step, imagePreviewSubmatch(source, match, 3))
}

func imageGenerationModelLabel(step, resolvedModel, displayName string) string {
	return displayName + `=` + resolvedModel + `?.displayName||((modelName)=>{let match=/^gpt-image-(\d+)$/i.exec(modelName||"");return match?"GPT Image "+match[1]:void 0})(` + step + `.modelName)||((modelName)=>{let match=/^gemini[-_](.+)$/i.exec(modelName||"");return match?"Gemini "+match[1].replace(/[-_]+/g," ").replace(/\b\w/g,character=>character.toUpperCase()):void 0})(` + step + `.modelName)||` + step + `.modelName||"Image generator"`
}

func addNeutralImageGenerationLoadingTitle(source, step, status string) (string, bool) {
	matches := imageGenerationTitleChildrenPattern.FindAllStringSubmatchIndex(source, -1)
	for _, match := range matches {
		if imagePreviewSubmatch(source, match, 3) != status {
			continue
		}
		newTitle := imagePreviewSubmatch(source, match, 1) + `("span",{children:` + imagePreviewSubmatch(source, match, 2) + `(` + status + `)&&!` + step + `.modelName?` + "`Generating image`:" + imagePreviewSubmatch(source, match, 2) + `(` + status + `)?`
		return source[:match[0]] + newTitle + source[match[1]:], true
	}
	return source, false
}

func upgradeLegacyImageGenerationUIRenderers(source string) (string, bool, bool) {
	titleMatches := imageGenerationLegacyUITitleRendererPattern.FindAllStringSubmatchIndex(source, -1)
	if len(titleMatches) == 0 {
		return source, false, false
	}
	replacements := make([]imagePreviewRendererReplacement, 0, len(titleMatches)*2)
	usedMarkers := make(map[int]bool)
	for _, match := range titleMatches {
		if !sameImagePreviewIdentifiers(
			imagePreviewSubmatch(source, match, 2),
			imagePreviewSubmatch(source, match, 5),
			imagePreviewSubmatch(source, match, 7),
			imagePreviewSubmatch(source, match, 9),
			imagePreviewSubmatch(source, match, 12),
			imagePreviewSubmatch(source, match, 13),
		) || !sameImagePreviewIdentifiers(
			imagePreviewSubmatch(source, match, 6),
			imagePreviewSubmatch(source, match, 11),
			imagePreviewSubmatch(source, match, 15),
		) {
			continue
		}
		endOffset := strings.Index(source[match[1]:], "})},")
		if endOffset < 0 || endOffset > 4*1024 {
			continue
		}
		end := match[1] + endOffset + len("})}")
		markerEnd := end + 8*1024
		if markerEnd > len(source) {
			markerEnd = len(source)
		}
		markerOffset := strings.Index(source[end:markerEnd], "/*"+imageGenerationUIPatchV1Marker+"*/")
		if markerOffset < 0 {
			continue
		}
		markerStart := end + markerOffset
		if usedMarkers[markerStart] {
			continue
		}
		updatedTitle, ok := upgradeLegacyImageGenerationTitleRenderer(source[match[0]:end], source, match)
		if !ok {
			continue
		}
		replacements = append(replacements,
			imagePreviewRendererReplacement{start: match[0], end: end, value: updatedTitle},
			imagePreviewRendererReplacement{start: markerStart, end: markerStart + len("/*"+imageGenerationUIPatchV1Marker+"*/"), value: "/*" + imageGenerationUIPatchMarker + "*/"},
		)
		usedMarkers[markerStart] = true
	}
	if len(replacements) == 0 {
		return source, false, false
	}
	sort.Slice(replacements, func(left, right int) bool {
		return replacements[left].start < replacements[right].start
	})
	var output strings.Builder
	last := 0
	for _, replacement := range replacements {
		if replacement.start < last {
			return source, false, false
		}
		output.WriteString(source[last:replacement.start])
		output.WriteString(replacement.value)
		last = replacement.end
	}
	output.WriteString(source[last:])
	return output.String(), true, true
}

func upgradeLegacyImageGenerationTitleRenderer(current, source string, match []int) (string, bool) {
	step := imagePreviewSubmatch(source, match, 2)
	status := imagePreviewSubmatch(source, match, 3)
	resolvedModel := imagePreviewSubmatch(source, match, 6)
	displayName := imagePreviewSubmatch(source, match, 10)
	isNewModel := imagePreviewSubmatch(source, match, 14)
	oldModelLabel := displayName + `=` + resolvedModel + `?.displayName||(` + step + `.modelName?.replace(/^gpt-image-(\d+)$/i,"GPT Image $1"))||` + step + `.modelName||"Image generator"`
	if strings.Count(current, oldModelLabel) != 1 {
		return "", false
	}
	updated := strings.Replace(current, oldModelLabel, imageGenerationModelLabel(step, resolvedModel, displayName), 1)
	oldIsNewModel := `,` + isNewModel + `=` + resolvedModel + `?.isNewModel??!1;return `
	newIsNewModel := `,` + isNewModel + `=` + resolvedModel + `?.isNewModel??!1,$wfIsGeminiImage=!!` + resolvedModel + `||/^gemini[-_]/i.test(` + step + `.modelName||"");return `
	if strings.Count(updated, oldIsNewModel) != 1 {
		return "", false
	}
	updated = strings.Replace(updated, oldIsNewModel, newIsNewModel, 1)
	for _, prefix := range []string{"Generating with ", "Generated with ", "Generate with "} {
		oldTitle := "`" + prefix + "${" + displayName + "}${" + resolvedModel + `?" \u{1F34C}":""}` + "`"
		newTitle := "`" + prefix + "${" + displayName + "}${$wfIsGeminiImage?\" \\u{1F34C}\":\"\"}`"
		if strings.Count(updated, oldTitle) != 1 {
			return "", false
		}
		updated = strings.Replace(updated, oldTitle, newTitle, 1)
	}
	return addNeutralImageGenerationLoadingTitle(updated, step, status)
}

func imageGenerationResultRendererReplacement(source string, match []int, hookSource string, hookMatch []int) string {
	component := imagePreviewSubmatch(source, match, 1)
	step := imagePreviewSubmatch(source, match, 2)
	status := imagePreviewSubmatch(source, match, 3)
	err := imagePreviewSubmatch(source, match, 4)
	jsx := imagePreviewSubmatch(source, match, 5)
	container := imagePreviewSubmatch(source, match, 6)
	loading := imagePreviewSubmatch(source, match, 7)
	title := imagePreviewSubmatch(source, match, 10)
	supplementaryView := imagePreviewSubmatch(source, match, 15)
	stateHook := imagePreviewSubmatch(hookSource, hookMatch, 3)
	callbackHook := imagePreviewSubmatch(hookSource, hookMatch, 5)

	return "/*" + imageGenerationUIPatchMarker + "*/" + component + "=({step:" + step + ",status:" + status + ",error:" + err + "})=>{let[$wfImageExpanded,$wfSetImageExpanded]=" + stateHook + "(!0),$wfToggleImageExpanded=" + callbackHook + "(()=>{$wfSetImageExpanded(value=>!value)},[]);return " + jsx + "(" + container + ",{loading:" + loading + "(" + status + "),title:" + jsx + "(" + title + ",{step:" + step + ",status:" + status + "}),supplementaryView:" + err + "?null:" + jsx + "(" + supplementaryView + ",{step:" + step + ",status:" + status + "}),cta:null,isExpanded:$wfImageExpanded,onToggle:$wfToggleImageExpanded,hasSupplementaryView:!" + err + "})}"
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
	return media + "?.uri?(" + image + "=" + resolver + "?.(" + media + ".uri)," + image + "=(" + image + "&&typeof " + image + ".getState===\"function\"?" + image + ".getState():" + image + ")," + image + "=typeof " + image + "===\"string\"?" + image + ":void 0):" +
		media + "?.payload?.case===\"inlineData\"&&(" + image + "=" + media + "?" + inlineData + "(" + media + "):void 0),!" + image + "&&" + media + "?.base64Data&&(" + image + "=\"data:\"+(" + media + ".mimeType||\"image/png\")+\";base64,\"+(typeof " + media + ".base64Data===\"string\"?" + media + ".base64Data:btoa(Array.from(" + media + ".base64Data).map(i=>String.fromCharCode(i)).join(\"\"))))" +
		imagePreviewFileURIFallback(media, image)
}

func imagePreviewFileURIFallback(media, image string) string {
	// The IIFE keeps the fallback's local names out of the minified renderer
	// scope. It intentionally catches malformed percent encodings: a bad file
	// name must not throw from React rendering and blank the whole chat step.
	// VS Code maps local files to vscode-file://vscode-app/...; that is a
	// same-origin browser resource, while raw paths and file: are rejected by
	// the workbench image CSP. UNC hosts remain URI authorities.
	return ",!" + image + "&&" + media + "?.uri&&typeof " + media + ".uri===\"string\"&&" + media + ".uri.startsWith(\"file://\")&&(" + image + "=((u)=>{let p=u.replace(/^file:\\/\\//,\"\");try{p=decodeURIComponent(p)}catch{}let h=p.match(/^([^/]+)(\\/.*)$/),e=v=>encodeURI(v).replace(/[?#]/g,c=>c===\"#\"?\"%23\":\"%3F\");return h&&h[1]!==\"localhost\"?\"vscode-file://\"+h[1]+e(h[2]):\"vscode-file://vscode-app\"+e(h&&h[1]===\"localhost\"?h[2]:p)})(" + media + ".uri))"
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

// The Electron main-process bundle is deliberately excluded. These are the
// two verified chat renderers that own the image tool UI in IDE builds.
func imageGenerationUIRendererPaths(root string) []string {
	paths := make([]string, 0, 2)
	for _, relative := range []string{
		"out/jetskiAgent/main.js",
		"out/vs/workbench/workbench.desktop.main.js",
	} {
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
