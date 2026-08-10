//go:build windows

package patcher

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
)

func TestReadAgentEmbeddedUIArchiveRecognizesVerifiedLayouts(t *testing.T) {
	tests := []struct {
		name        string
		entries     []string
		stylesheets []string
		wantError   bool
	}{
		{name: "legacy jetbox", entries: []string{"index.html", "main.js", "jetbox.css"}, stylesheets: []string{"jetbox.css"}},
		{name: "current tailwind", entries: []string{"index.html", "main.js", "compiled_tailwind.css"}, stylesheets: []string{"compiled_tailwind.css"}},
		{name: "hybrid", entries: []string{"index.html", "main.js", "jetbox.css", "compiled_tailwind.css"}, stylesheets: []string{"compiled_tailwind.css", "jetbox.css"}},
		{name: "unknown stylesheet", entries: []string{"index.html", "main.js", "future.css"}, wantError: true},
		{name: "duplicate main", entries: []string{"index.html", "main.js", "main.js", "jetbox.css"}, wantError: true},
		{name: "duplicate index", entries: []string{"index.html", "index.html", "main.js", "jetbox.css"}, wantError: true},
		{name: "duplicate known stylesheet", entries: []string{"index.html", "main.js", "jetbox.css", "jetbox.css"}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := buildAgentEmbeddedUIFixture(t, test.entries)
			archive, err := readAgentEmbeddedUIArchive(data)
			if test.wantError {
				if err == nil {
					t.Fatalf("unsafe layout was accepted: stylesheets=%v", archive.stylesheets)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(archive.stylesheets, ",") != strings.Join(test.stylesheets, ",") {
				t.Fatalf("stylesheets=%v, want %v", archive.stylesheets, test.stylesheets)
			}
			if archive.start != len("verified-pe-prefix") || archive.end >= len(data) {
				t.Fatalf("archive bounds=(%d,%d), data=%d", archive.start, archive.end, len(data))
			}
		})
	}
}

func buildAgentEmbeddedUIFixture(t *testing.T, entries []string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("fixture:" + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data := append([]byte("verified-pe-prefix"), buffer.Bytes()...)
	return append(data, []byte("verified-pe-suffix")...)
}

func TestListAgentEmbeddedArchivesWhenRequested(t *testing.T) {
	path := os.Getenv("ANTIGRAVITY_WF_TEST_AGENT_LANGUAGE_SERVER")
	if path == "" {
		t.Skip("official Agent language server fixture is not configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	valid := 0
	for searchEnd := len(data); searchEnd >= len(zipEndOfCentralDirectorySignature); {
		relative := bytes.LastIndex(data[:searchEnd], zipEndOfCentralDirectorySignature)
		if relative < 0 {
			break
		}
		searchEnd = relative
		if relative+22 > len(data) {
			continue
		}
		commentLength := int(binary.LittleEndian.Uint16(data[relative+20 : relative+22]))
		archiveEnd := relative + 22 + commentLength
		centralSize := int(binary.LittleEndian.Uint32(data[relative+12 : relative+16]))
		centralOffset := int(binary.LittleEndian.Uint32(data[relative+16 : relative+20]))
		archiveStart := relative - centralSize - centralOffset
		if archiveStart < 0 || archiveEnd > len(data) || archiveStart >= archiveEnd {
			continue
		}
		reader, readErr := zip.NewReader(bytes.NewReader(data[archiveStart:archiveEnd]), int64(archiveEnd-archiveStart))
		if readErr != nil {
			continue
		}
		valid++
		var relevant []string
		for _, entry := range reader.File {
			if !strings.Contains(entry.Name, "/") && (strings.HasSuffix(entry.Name, ".js") || strings.HasSuffix(entry.Name, ".css") || strings.HasSuffix(entry.Name, ".html")) {
				relevant = append(relevant, entry.Name)
			}
		}
		t.Logf("ZIP start=%d bytes=%d entries=%d topLevel=%s", archiveStart, archiveEnd-archiveStart, len(reader.File), strings.Join(relevant, ","))
	}
	if valid == 0 {
		t.Fatal("no readable embedded ZIP archive found")
	}
}

func TestOfficialAgentEmbeddedUIArchiveWhenFixturePresent(t *testing.T) {
	path := os.Getenv("ANTIGRAVITY_WF_TEST_AGENT_LANGUAGE_SERVER")
	if path == "" {
		t.Skip("official Agent language server fixture is not configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := readAgentEmbeddedUIArchive(data)
	if err != nil {
		t.Fatalf("Agent language server embedded UI archive is unreadable: %v", err)
	}
	entries := map[string]*zip.File{}
	for _, entry := range archive.reader.File {
		entries[entry.Name] = entry
	}
	requiredEntries := append([]string{"index.html", "main.js"}, archive.stylesheets...)
	for _, required := range requiredEntries {
		if entries[required] == nil {
			t.Fatalf("Agent embedded UI archive is missing %s", required)
		}
	}
	reader, err := entries["main.js"].Open()
	if err != nil {
		t.Fatal(err)
	}
	mainData, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	mainSource := string(mainData)
	for _, required := range []string{"Generated with", "Generated image preview", "Artifact image", "gemini-3.1-flash-image"} {
		if !strings.Contains(mainSource, required) {
			t.Fatalf("Agent embedded UI main.js is missing verified structure %q", required)
		}
	}
	digest := sha256.Sum256(mainData)
	t.Logf("archiveOffset=%d archiveBytes=%d entries=%d stylesheets=%s mainBytes=%d mainSHA256=%s", archive.start, archive.end-archive.start, len(entries), strings.Join(archive.stylesheets, ","), len(mainData), strings.ToUpper(hex.EncodeToString(digest[:])))
	titleRaw := agentImageGenerationTitlePattern.FindAllStringSubmatchIndex(mainSource, -1)
	t.Logf("Agent title raw pattern matches=%d generated previews=%d markdown renderers=%d", len(titleRaw), len(allStringOffsets(mainSource, `alt:"Generated image preview"`)), len(agentImageArtifactMarkdownRendererPrefixPattern.FindAllStringSubmatchIndex(mainSource, -1)))
	if len(titleRaw) == 1 {
		var groups []string
		for group := 1; group <= 14; group++ {
			groups = append(groups, imagePreviewSubmatch(mainSource, titleRaw[0], group))
		}
		t.Logf("Agent title groups=%q", groups)
	}
	for _, candidate := range agentImageArtifactMarkdownRendererPrefixPattern.FindAllStringSubmatchIndex(mainSource, -1) {
		end := agentMinifiedDeclarationEnd(mainSource, candidate[1], 8*1024)
		if end < 0 {
			continue
		}
		segment := mainSource[candidate[1]:end]
		component := imagePreviewSubmatch(mainSource, candidate, 1)
		sourceValue := imagePreviewSubmatch(mainSource, candidate, 2)
		alt := imagePreviewSubmatch(mainSource, candidate, 3)
		errorState := imagePreviewSubmatch(mainSource, candidate, 8)
		t.Logf("Agent artifact candidate=%s artifact=%t ifReturn=%d ternaryReturn=%d", component,
			strings.Contains(segment, `alt:`+alt+`||"Artifact image"`),
			strings.Count(segment, `;if(!`+sourceValue+`||`+errorState+`)return `),
			strings.Count(segment, `;return!`+sourceValue+`||`+errorState+`?`))
	}
	titleProbe, titleRecognized, titleChanged := patchAgentImageGenerationTitle(mainSource)
	_, dedupeRecognized, dedupeChanged := patchAgentDuplicateGeneratedImage(titleProbe)
	t.Logf("Agent UI probes: titleRecognized=%t titleChanged=%t dedupeRecognized=%t dedupeChanged=%t", titleRecognized, titleChanged, dedupeRecognized, dedupeChanged)

	patchedData, result, err := patchAgentEmbeddedUIArchive(data)
	if err != nil {
		t.Fatalf("patch verified Agent embedded UI archive: %v", err)
	}
	if !result.ArchiveRecognized || !result.UIRecognized || !result.Changed {
		t.Fatalf("verified Agent UI patch result = %+v", result)
	}
	if len(patchedData) != len(data) {
		t.Fatalf("patched language server length = %d, want %d", len(patchedData), len(data))
	}
	if !bytes.Equal(patchedData[:archive.start], data[:archive.start]) ||
		!bytes.Equal(patchedData[archive.end:], data[archive.end:]) {
		t.Fatal("Agent UI patch changed PE bytes outside the embedded ZIP")
	}
	patchedArchive, err := readAgentEmbeddedUIArchive(patchedData)
	if err != nil {
		t.Fatal(err)
	}
	patchedMain, err := readAgentEmbeddedUIEntry(patchedArchive, "main.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{agentImageGenerationUIPatchMarker, agentImageGenerationDedupePatchMarker, imageGenerationUIPatchMarker} {
		if !bytes.Contains(patchedMain, []byte(marker)) {
			t.Fatalf("patched Agent main.js is missing %s", marker)
		}
	}
	if !bytes.Contains(patchedMain, []byte(`match?"GPT Image "+match[1]`)) ||
		!bytes.Contains(patchedMain, []byte(`$wfIsDuplicateGeneratedImageURI`)) {
		t.Fatal("patched Agent main.js is missing the model label or exact-URI dedupe behavior")
	}
	second, secondResult, err := patchAgentEmbeddedUIArchive(patchedData)
	if err != nil || !secondResult.UIRecognized || secondResult.Changed || !bytes.Equal(second, patchedData) {
		t.Fatalf("Agent UI patch is not idempotent: result=%+v err=%v equal=%t", secondResult, err, bytes.Equal(second, patchedData))
	}
}

func TestExportAgentEmbeddedMainWhenRequested(t *testing.T) {
	languageServer := os.Getenv("ANTIGRAVITY_WF_TEST_AGENT_LANGUAGE_SERVER")
	output := os.Getenv("ANTIGRAVITY_WF_TEST_AGENT_MAIN_OUTPUT")
	if languageServer == "" || output == "" {
		t.Skip("patched Agent main.js export is not configured")
	}
	data, err := os.ReadFile(languageServer)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := readAgentEmbeddedUIArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	mainData, err := readAgentEmbeddedUIEntry(archive, "main.js")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, mainData, 0o600); err != nil {
		t.Fatal(err)
	}
}
