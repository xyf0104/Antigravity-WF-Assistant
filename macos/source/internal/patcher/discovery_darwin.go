//go:build darwin

package patcher

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const darwinDiscoveryCommandTimeout = 2 * time.Second

// Keep the process expression deliberately narrow: the captured bundle must
// itself have an Antigravity/Agent Window-looking name.  A parent directory
// named "Antigravity" must never make an unrelated Electron app a patch
// candidate.
var runningAppPattern = regexp.MustCompile(`(?i)(/[^\n]*?(?:antigravity|agent[^\n/]*window)[^\n/]*\.app)/Contents/`)

func locateDarwinInstallations() []darwinTargets {
	candidates, explicit := darwinAppCandidates()
	return selectDarwinInstallations(candidates, explicit, trustedDarwinInstallLocation)
}

// selectDarwinInstallations separates candidate gathering from structural
// selection.  It is intentionally conservative: only user-supplied explicit
// paths may bypass the normal trusted-location/identity gates.  Process and
// Spotlight discoveries remain ordinary discoveries, so a stray copy cannot
// cause a standard installation to be patched as well.
func selectDarwinInstallations(candidates []string, explicit map[string]bool, isTrusted func(string) bool) []darwinTargets {
	seen := map[string]bool{}
	var targets []darwinTargets
	var deferredCustomLocations []string
	for _, candidate := range candidates {
		candidate = normalizeAppBundlePath(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if !explicit[candidate] && !isTrusted(candidate) {
			deferredCustomLocations = append(deferredCustomLocations, candidate)
			continue
		}
		if !explicit[candidate] && !darwinTrustedCandidateHasExpectedIdentity(candidate) {
			continue
		}
		if target, ok := inspectDarwinApp(candidate); ok {
			targets = append(targets, target)
		}
	}
	// A standard installation wins over stray copies found by Spotlight. If no
	// standard installation exists, accept a structurally valid Google
	// Antigravity bundle from a custom location.
	if len(targets) == 0 {
		for _, candidate := range deferredCustomLocations {
			if !darwinBundleIdentifierIsAntigravity(candidate) {
				continue
			}
			if target, ok := inspectDarwinApp(candidate); ok {
				targets = append(targets, target)
			}
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		left, right := darwinCandidateRank(targets[i].app), darwinCandidateRank(targets[j].app)
		if left != right {
			return left < right
		}
		if targets[i].kind != targets[j].kind {
			return targets[i].kind < targets[j].kind
		}
		return targets[i].app < targets[j].app
	})
	return targets
}

func darwinAppCandidates() ([]string, map[string]bool) {
	var candidates []string
	explicit := map[string]bool{}
	addExplicit := func(value string) {
		for _, item := range filepath.SplitList(value) {
			if path := normalizeAppBundlePath(strings.TrimSpace(item)); path != "" {
				candidates = append(candidates, path)
				explicit[path] = true
			}
		}
	}
	addExplicit(os.Getenv("ANTIGRAVITY_APP_PATH"))
	addExplicit(os.Getenv("ANTIGRAVITY_APP_PATHS"))
	// Explicit paths are also the test and recovery boundary. If the caller
	// supplies one, never silently fall back to a different system install.
	if len(explicit) > 0 {
		return candidates, explicit
	}

	roots := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	for _, root := range roots {
		candidates = append(candidates, darwinApplicationsInRoot(root)...)
	}

	if output, err := darwinDiscoveryOutput("ps", "-axo", "command="); err == nil {
		for _, match := range runningAppPattern.FindAllStringSubmatch(string(output), -1) {
			if len(match) > 1 {
				path := normalizeAppBundlePath(match[1])
				if path != "" {
					// A process path is useful discovery evidence, not an explicit
					// user instruction.  Keep the same safety gates as Spotlight.
					candidates = append(candidates, path)
				}
			}
		}
	}
	if path, err := exec.LookPath("mdfind"); err == nil {
		query := `(kMDItemContentType == "com.apple.application-bundle"c) && (kMDItemFSName == "*Antigravity*.app"c || kMDItemFSName == "*Agent*Window*.app"c)`
		if output, err := darwinDiscoveryOutput(path, query); err == nil {
			for _, line := range strings.Split(string(output), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					candidates = append(candidates, line)
				}
			}
		}
	}
	return candidates, explicit
}

// darwinApplicationsInRoot performs only a one-level directory read.  It
// avoids brittle Glob case patterns (which miss names such as
// "Google ANTIGRAVITY.app" on case-sensitive volumes) without recursively
// scanning a user's disk.
func darwinApplicationsInRoot(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	paths := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !darwinAppNameLooksLikeAntigravity(entry.Name()) {
			continue
		}
		paths = append(paths, filepath.Join(root, entry.Name()))
	}
	sort.Strings(paths)
	return paths
}

func darwinAppNameLooksLikeAntigravity(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if !strings.HasSuffix(lower, ".app") {
		return false
	}
	return strings.Contains(lower, "antigravity") ||
		(strings.Contains(lower, "agent") && strings.Contains(lower, "window"))
}

func darwinBundleIdentifierIsAntigravity(path string) bool {
	return strings.Contains(strings.ToLower(darwinBundleValue(path, "CFBundleIdentifier")), "antigravity")
}

func darwinTrustedCandidateHasExpectedIdentity(path string) bool {
	return darwinAppNameLooksLikeAntigravity(filepath.Base(path)) || darwinBundleIdentifierIsAntigravity(path)
}

func darwinDiscoveryOutput(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), darwinDiscoveryCommandTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

func normalizeAppBundlePath(path string) string {
	path = strings.Trim(strings.TrimSpace(path), `"`)
	if path == "" {
		return ""
	}
	lower := strings.ToLower(path)
	if index := strings.Index(lower, ".app/"); index >= 0 {
		path = path[:index+4]
	}
	if !strings.HasSuffix(strings.ToLower(path), ".app") {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func trustedDarwinInstallLocation(path string) bool {
	if strings.HasPrefix(path, "/Applications/") || strings.HasPrefix(path, "/System/Applications/") {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, filepath.Join(home, "Applications")+string(os.PathSeparator)) {
		return true
	}
	return false
}

func darwinCandidateRank(path string) int {
	if strings.HasPrefix(path, "/Applications/") {
		return 0
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, filepath.Join(home, "Applications")+string(os.PathSeparator)) {
		return 1
	}
	return 2
}

func inspectDarwinApp(appPath string) (darwinTargets, bool) {
	info, err := os.Stat(appPath)
	if err != nil || !info.IsDir() {
		return darwinTargets{}, false
	}
	resources := filepath.Join(appPath, "Contents", "Resources")
	unpacked := filepath.Join(resources, "app")
	mainPath := existingFile(filepath.Join(unpacked, "out", "main.js"))
	extensionRoot := filepath.Join(unpacked, "extensions", "antigravity")
	extensionEntry := existingFile(filepath.Join(extensionRoot, "dist", "extension.js"))
	// A few unpacked releases ship the Language Server under the extension
	// directory but no independently patchable extension.js.  Do not clear
	// extensionRoot before checking bin/, otherwise the lookup accidentally
	// falls back to a relative "bin" in the helper's current directory and
	// incorrectly reports a supported installation as missing.
	languagePath := locateDarwinLanguageServer(filepath.Join(extensionRoot, "bin"))
	extensionDir := extensionRoot
	if extensionEntry == "" {
		extensionDir = ""
	}
	// Newer IDE releases can move the Language Server into an Electron entry
	// script.  An unpacked IDE remains a supported target without a separate
	// binary only when its independently patchable extension entry is present;
	// applyDarwinPatch still verifies both source shapes before writing.
	if mainPath != "" && (extensionEntry != "" || languagePath != "") {
		return darwinTargets{
			app: appPath, name: strings.TrimSuffix(filepath.Base(appPath), filepath.Ext(appPath)),
			kind: "ide", version: darwinBundleValue(appPath, "CFBundleShortVersionString"),
			main: mainPath, extension: extensionDir, extensionEntry: extensionEntry, language: languagePath,
		}, true
	}

	asarPath := existingFile(filepath.Join(resources, "app.asar"))
	if asarPath == "" {
		return darwinTargets{}, false
	}
	for _, binDir := range []string{filepath.Join(resources, "bin"), filepath.Join(resources, "app.asar.unpacked", "bin")} {
		if languagePath = locateDarwinLanguageServer(binDir); languagePath != "" {
			break
		}
	}
	// A packaged Agent may carry the whole launch path in app.asar.  Only
	// accept the no-binary variant when both known entries are present and the
	// launcher contains a vendor/current helper endpoint that the ASAR patcher
	// can prove it knows how to replace.  Unknown archives are deliberately not
	// surfaced as patch targets.
	if languagePath == "" && !darwinASARHasSupportedEntrypoints(asarPath) {
		return darwinTargets{}, false
	}
	return darwinTargets{
		app: appPath, name: strings.TrimSuffix(filepath.Base(appPath), filepath.Ext(appPath)),
		kind: "agent", version: darwinBundleValue(appPath, "CFBundleShortVersionString"),
		asar: asarPath, language: languagePath,
	}, true
}

// darwinASARHasSupportedEntrypoints is a discovery-time safety gate for the
// packaged Agent layout that no longer ships a standalone language_server.
// It mirrors prepareDarwinASARCandidate's two required entries without
// attempting a write: main.js must be the known CommonJS/managed entry and
// languageServer.js must still expose either the vendor endpoint or a
// previously managed local endpoint.  This prevents a broad "any app.asar"
// fallback while allowing a verified entry-script-only release to be patched.
func darwinASARHasSupportedEntrypoints(asarPath string) bool {
	archive, err := readASAR(asarPath)
	if err != nil {
		return false
	}
	main, err := archive.readFile("dist/main.js")
	if err != nil {
		return false
	}
	launcher, err := archive.readFile("dist/languageServer.js")
	if err != nil {
		return false
	}
	managedMain := bytes.Contains(main, []byte(darwinASARMarker))
	if !managedMain && !bytes.HasPrefix(main, []byte(`"use strict";`)) {
		return false
	}
	endpoint := currentPatchProxyEndpoint()
	if darwinCloudCodeURLPattern.Match(launcher) || bytes.Contains(launcher, []byte(endpoint.Base)) {
		return true
	}
	// A previously managed archive may use a staged fallback port that is no
	// longer the process-local endpoint. It is safe to migrate only because the
	// main entry carries our explicit marker and apply will rebuild from its
	// canonical vendor backup.
	return managedMain && bytes.Contains(launcher, []byte("http://127.0.0.1:"))
}

func darwinBundleValue(appPath, key string) string {
	plist := filepath.Join(appPath, "Contents", "Info.plist")
	if output, err := darwinDiscoveryOutput("/usr/bin/plutil", "-extract", key, "raw", "-o", "-", plist); err == nil {
		return strings.TrimSpace(string(output))
	}
	return ""
}

func locateDarwinLanguageServer(binDir string) string {
	if binDir == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(binDir, "language_server_macos*"))
	sort.Strings(matches)
	preferred := "x64"
	if runtime.GOARCH == "arm64" {
		preferred = "arm64"
	}
	for _, path := range matches {
		if strings.Contains(filepath.Base(path), preferred) && existingFile(path) != "" {
			return path
		}
	}
	for _, path := range matches {
		if existingFile(path) != "" {
			return path
		}
	}
	return ""
}
