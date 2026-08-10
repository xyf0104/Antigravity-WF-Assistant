//go:build windows

package patcher

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestOfficialAgentStructureWhenFixturePresent documents the current official
// Agent ASAR surface without mutating it. A separate connection adapter is
// required unless this test finds the exact native configuration chain.
func TestOfficialAgentStructureWhenFixturePresent(t *testing.T) {
	root := os.Getenv("ANTIGRAVITY_WF_TEST_AGENT_ROOT")
	if root == "" {
		t.Skip("official Agent fixture is not configured")
	}
	archive, err := readASAR(filepath.Join(root, "resources", "app.asar"))
	if err != nil {
		t.Fatal(err)
	}
	paths := collectASARFiles(archive.root, "")
	var relevant []string
	cloudCodeSetting, cloudEndpoint, helperMarker := false, false, false
	imageRendererMatches := 0
	for _, path := range paths {
		if !strings.HasSuffix(path, ".js") {
			continue
		}
		data, readErr := archive.readFile(path)
		if readErr != nil {
			continue
		}
		source := string(data)
		if strings.Contains(source, windowsCloudCodeSetting) {
			cloudCodeSetting = true
			relevant = append(relevant, path+":setting")
		}
		if strings.Contains(source, "--cloud_code_endpoint") {
			cloudEndpoint = true
			relevant = append(relevant, path+":endpoint")
		}
		if windowsContainsKnownPatch(data) {
			helperMarker = true
			relevant = append(relevant, path+":helper-marker")
		}
		if _, result := patchImagePreviewRenderer(source); result.Recognized {
			imageRendererMatches++
			relevant = append(relevant, path+":image-renderer")
		}
	}
	if helperMarker {
		sort.Strings(relevant)
		t.Fatalf("official Agent fixture unexpectedly contains a WF marker: %s", strings.Join(relevant, ","))
	}
	target := windowsTarget{
		root:     root,
		kind:     "agent",
		asar:     filepath.Join(root, "resources", "app.asar"),
		language: filepath.Join(root, "resources", "bin", "language_server.exe"),
	}
	supported, mode, reason := windowsTargetConnectionSupport(target)
	if !supported {
		t.Fatalf("official Agent fixture does not satisfy the minimal adapter: %s", reason)
	}
	sort.Strings(relevant)
	t.Logf("asar files=%d nativeSetting=%t nativeEndpoint=%t imageRendererMatches=%d connectionMode=%s relevant=%s", len(paths), cloudCodeSetting, cloudEndpoint, imageRendererMatches, mode, strings.Join(relevant, ","))
}

// TestOfficialAgentBrowserSurfaceWhenFixturePresent records where the Agent
// obtains its visible UI. Agent 2.x does not bundle the IDE chat renderer in
// app.asar, so renderer patching must not be guessed from the IDE layout.
func TestOfficialAgentBrowserSurfaceWhenFixturePresent(t *testing.T) {
	root := os.Getenv("ANTIGRAVITY_WF_TEST_AGENT_ROOT")
	if root == "" {
		t.Skip("official Agent fixture is not configured")
	}
	archive, err := readASAR(filepath.Join(root, "resources", "app.asar"))
	if err != nil {
		t.Fatal(err)
	}
	packageData, err := archive.readFile("package.json")
	if err != nil {
		t.Fatal(err)
	}
	var packageInfo struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(packageData, &packageInfo); err != nil {
		t.Fatal(err)
	}
	mainData, err := archive.readFile("dist/main.js")
	if err != nil {
		t.Fatal(err)
	}
	loadURLPattern := regexp.MustCompile(`(?s).{0,180}(?:loadURL|loadFile)\(.{0,320}`)
	matches := loadURLPattern.FindAllString(string(mainData), 8)
	for index := range matches {
		matches[index] = strings.Join(strings.Fields(matches[index]), " ")
	}
	if len(matches) == 0 {
		t.Fatal("official Agent main process has no verified browser loading surface")
	}
	mainSource := string(mainData)
	var urlOrigins []string
	for _, needle := range []string{"const newUrl", "let newUrl", "const url =", "loadURL(url)"} {
		if index := strings.Index(mainSource, needle); index >= 0 {
			start, end := index-240, index+560
			if start < 0 {
				start = 0
			}
			if end > len(mainSource) {
				end = len(mainSource)
			}
			urlOrigins = append(urlOrigins, strings.Join(strings.Fields(mainSource[start:end]), " "))
		}
	}
	t.Logf("packageVersion=%s browserLoads=%s urlOrigins=%s", packageInfo.Version, strings.Join(matches, " || "), strings.Join(urlOrigins, " || "))
}

// TestMutableAgentApplyWhenFixturePresent is an opt-in real-file regression
// test. Its root must be an isolated copy, never an installed product.
func TestMutableAgentApplyWhenFixturePresent(t *testing.T) {
	root := os.Getenv("ANTIGRAVITY_WF_MUTABLE_TEST_AGENT_ROOT")
	if root == "" {
		t.Skip("mutable Agent fixture is not configured")
	}
	if strings.EqualFold(filepath.Clean(root), filepath.Clean(os.Getenv("LOCALAPPDATA")+`\Programs\antigravity`)) {
		t.Fatal("refusing to mutate the official Antigravity installation")
	}
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", filepath.Join(t.TempDir(), "backups"))
	target := windowsTarget{
		root:       root,
		name:       "isolated Antigravity Agent",
		kind:       "agent",
		executable: filepath.Join(root, "Antigravity.exe"),
		asar:       filepath.Join(root, "resources", "app.asar"),
		language:   filepath.Join(root, "resources", "bin", "language_server.exe"),
	}
	if _, err := applyWindowsASARTarget(target); err != nil {
		t.Fatal(err)
	}
	if _, _, _, patched := windowsTargetPatchState(target); !patched {
		t.Fatal("isolated Agent did not pass the post-write patch state check")
	}
}

// TestOfficialAgentASARCandidateWhenFixturePresent validates a candidate in a
// temporary directory. It never writes to the official installation fixture.
func TestOfficialAgentASARCandidateWhenFixturePresent(t *testing.T) {
	root := os.Getenv("ANTIGRAVITY_WF_TEST_AGENT_ROOT")
	if root == "" {
		t.Skip("official Agent fixture is not configured")
	}
	source := filepath.Join(root, "resources", "app.asar")
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := prepareWindowsASARCandidate(source, filepath.Join(t.TempDir(), "app.asar"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(candidate)
	if !windowsASARPatched(candidate) {
		t.Fatal("read-only official fixture did not produce a complete candidate")
	}
	sourceArchive, err := readASAR(source)
	if err != nil {
		t.Fatal(err)
	}
	candidateArchive, err := readASAR(candidate)
	if err != nil {
		t.Fatal(err)
	}
	sourceMain, err := sourceArchive.readFile("dist/main.js")
	if err != nil {
		t.Fatal(err)
	}
	candidateMain, err := candidateArchive.readFile("dist/main.js")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sourceMain, candidateMain) {
		t.Fatal("minimal Agent candidate unexpectedly changed dist/main.js")
	}
	after, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("official fixture was modified while preparing a candidate")
	}
}

func collectASARFiles(node *asarNode, prefix string) []string {
	if node == nil {
		return nil
	}
	keys := make([]string, 0, len(node.Files))
	for key := range node.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var paths []string
	for _, key := range keys {
		child := node.Files[key]
		path := strings.TrimPrefix(prefix+"/"+key, "/")
		if child.Files != nil {
			paths = append(paths, collectASARFiles(child, path)...)
			continue
		}
		paths = append(paths, path)
	}
	return paths
}
