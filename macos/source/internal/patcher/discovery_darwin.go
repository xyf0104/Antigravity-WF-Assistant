//go:build darwin

package patcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var runningAppPattern = regexp.MustCompile(`(?i)(/[^\n]*?antigravity[^\n]*?\.app)/Contents/`)

func locateDarwinInstallations() []darwinTargets {
	candidates, explicit := darwinAppCandidates()
	seen := map[string]bool{}
	var targets []darwinTargets
	var deferredCustomLocations []string
	for _, candidate := range candidates {
		candidate = normalizeAppBundlePath(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if !explicit[candidate] && !trustedDarwinInstallLocation(candidate) {
			deferredCustomLocations = append(deferredCustomLocations, candidate)
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
			identifier := strings.ToLower(darwinBundleValue(candidate, "CFBundleIdentifier"))
			if !strings.Contains(identifier, "antigravity") {
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
		matches, _ := filepath.Glob(filepath.Join(root, "*[Aa]ntigravity*.app"))
		candidates = append(candidates, matches...)
		matches, _ = filepath.Glob(filepath.Join(root, "*[Aa]gent*[Ww]indow*.app"))
		candidates = append(candidates, matches...)
	}

	if output, err := exec.Command("ps", "-axo", "command=").Output(); err == nil {
		for _, match := range runningAppPattern.FindAllStringSubmatch(string(output), -1) {
			if len(match) > 1 {
				path := normalizeAppBundlePath(match[1])
				candidates = append(candidates, path)
				explicit[path] = true
			}
		}
	}
	if path, err := exec.LookPath("mdfind"); err == nil {
		query := `kMDItemContentType == "com.apple.application-bundle"c && kMDItemFSName == "*Antigravity*.app"c`
		if output, err := exec.Command(path, query).Output(); err == nil {
			for _, line := range strings.Split(string(output), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					candidates = append(candidates, line)
				}
			}
		}
	}
	return candidates, explicit
}

func normalizeAppBundlePath(path string) string {
	path = strings.TrimSpace(path)
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
	extensionDir := filepath.Join(unpacked, "extensions", "antigravity")
	extensionEntry := existingFile(filepath.Join(extensionDir, "dist", "extension.js"))
	if extensionEntry == "" {
		extensionDir = ""
	}
	languagePath := locateDarwinLanguageServer(filepath.Join(extensionDir, "bin"))
	if mainPath != "" && languagePath != "" {
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
	if languagePath == "" {
		return darwinTargets{}, false
	}
	return darwinTargets{
		app: appPath, name: strings.TrimSuffix(filepath.Base(appPath), filepath.Ext(appPath)),
		kind: "agent", version: darwinBundleValue(appPath, "CFBundleShortVersionString"),
		asar: asarPath, language: languagePath,
	}, true
}

func darwinBundleValue(appPath, key string) string {
	plist := filepath.Join(appPath, "Contents", "Info.plist")
	if output, err := exec.Command("/usr/bin/plutil", "-extract", key, "raw", "-o", "-", plist).Output(); err == nil {
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
