//go:build windows

package patcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	winapi "golang.org/x/sys/windows"
)

const windowsShellDiscoveryTimeout = 2 * time.Second

const windowsDiscoveryCacheTTL = 2 * time.Minute

var windowsDiscoveryCache = struct {
	sync.Mutex
	targets []windowsTarget
	at      time.Time
	deep    bool
}{}

func locateWindowsInstallations() []windowsTarget {
	// Normal status/apply calls use deterministic standard and previously
	// confirmed paths. PowerShell/CIM discovery is reserved for an explicit
	// refresh so reopening the assistant or reconnecting an unchanged install
	// never waits on process and registry enumeration.
	return locateWindowsInstallationsCached(false, false)
}

func locateWindowsInstallationsFast() []windowsTarget {
	return locateWindowsInstallationsCached(false, false)
}

func refreshWindowsInstallations() []windowsTarget {
	return locateWindowsInstallationsCached(true, true)
}

func locateWindowsInstallationsCached(includeShell, force bool) []windowsTarget {
	windowsDiscoveryCache.Lock()
	if !force && time.Since(windowsDiscoveryCache.at) < windowsDiscoveryCacheTTL &&
		len(windowsDiscoveryCache.targets) > 0 && (!includeShell || windowsDiscoveryCache.deep) {
		targets := append([]windowsTarget(nil), windowsDiscoveryCache.targets...)
		windowsDiscoveryCache.Unlock()
		return targets
	}
	windowsDiscoveryCache.Unlock()

	targets := inspectWindowsInstallCandidates(includeShell)
	windowsDiscoveryCache.Lock()
	// A fast scan must not replace a newer deep result. It is only the
	// responsive startup snapshot shown while the full refresh runs.
	if includeShell || !windowsDiscoveryCache.deep || time.Since(windowsDiscoveryCache.at) >= windowsDiscoveryCacheTTL {
		windowsDiscoveryCache.targets = append([]windowsTarget(nil), targets...)
		windowsDiscoveryCache.at = time.Now()
		windowsDiscoveryCache.deep = includeShell
	}
	result := append([]windowsTarget(nil), windowsDiscoveryCache.targets...)
	windowsDiscoveryCache.Unlock()
	return result
}

func invalidateWindowsDiscoveryCache() {
	windowsDiscoveryCache.Lock()
	windowsDiscoveryCache.targets = nil
	windowsDiscoveryCache.at = time.Time{}
	windowsDiscoveryCache.deep = false
	windowsDiscoveryCache.Unlock()
}

func inspectWindowsInstallCandidates(includeShell bool) []windowsTarget {
	candidates, explicit := windowsInstallCandidates(includeShell)
	seenRoots := map[string]bool{}
	seenTargets := map[string]bool{}
	var targets []windowsTarget
	for _, candidate := range candidates {
		root := normalizeWindowsInstallRoot(candidate)
		key := strings.ToLower(root)
		if root == "" || seenRoots[key] {
			continue
		}
		seenRoots[key] = true
		if target, ok := inspectWindowsInstall(root); ok {
			targetKey := strings.ToLower(target.kind + "|" + target.root)
			if !seenTargets[targetKey] {
				target.version = windowsVersionFromTarget(target)
				targets = append(targets, target)
				seenTargets[targetKey] = true
			}
		}
	}
	// Explicit paths form a hard safety boundary.
	if explicit && len(targets) == 0 {
		return nil
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if windowsTargetRank(targets[i]) != windowsTargetRank(targets[j]) {
			return windowsTargetRank(targets[i]) < windowsTargetRank(targets[j])
		}
		return strings.ToLower(targets[i].root) < strings.ToLower(targets[j].root)
	})
	return targets
}

func windowsInstallCandidates(includeShell bool) ([]string, bool) {
	var candidates []string
	add := func(values ...string) {
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				candidates = append(candidates, value)
			}
		}
	}
	for _, variable := range []string{"ANTIGRAVITY_APP_PATH", "ANTIGRAVITY_APP_PATHS"} {
		for _, value := range filepath.SplitList(os.Getenv(variable)) {
			add(value)
		}
	}
	if len(candidates) > 0 {
		return candidates, true
	}
	add(windowsSavedInstallPaths()...)

	for _, base := range []string{
		os.Getenv("LOCALAPPDATA"), os.Getenv("PROGRAMFILES"), os.Getenv("ProgramFiles"),
		os.Getenv("PROGRAMFILES(X86)"), os.Getenv("ProgramW6432"),
	} {
		if base == "" {
			continue
		}
		add(
			filepath.Join(base, "Programs", "antigravity"),
			filepath.Join(base, "Programs", "Antigravity"),
			filepath.Join(base, "Programs", "Antigravity IDE"),
			filepath.Join(base, "Programs", "Antigravity 2"),
			filepath.Join(base, "Programs", "Antigravity 2.0"),
			filepath.Join(base, "Programs", "Antigravity Agent"),
			filepath.Join(base, "Programs", "Google Antigravity"),
			filepath.Join(base, "Programs", "Google Antigravity IDE"),
			filepath.Join(base, "antigravity"),
			filepath.Join(base, "Antigravity"),
			filepath.Join(base, "Antigravity IDE"),
			filepath.Join(base, "Antigravity 2"),
			filepath.Join(base, "Antigravity 2.0"),
			filepath.Join(base, "Antigravity Agent"),
			filepath.Join(base, "Google Antigravity"),
			filepath.Join(base, "Google Antigravity IDE"),
		)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(
			filepath.Join(home, "AppData", "Local", "Programs", "antigravity"),
			filepath.Join(home, "AppData", "Local", "Programs", "Antigravity IDE"),
			filepath.Join(home, "AppData", "Local", "Programs", "Antigravity 2"),
			filepath.Join(home, "AppData", "Local", "Programs", "Antigravity 2.0"),
			filepath.Join(home, "AppData", "Local", "Programs", "Antigravity Agent"),
			filepath.Join(home, "AppData", "Local", "Programs", "Google Antigravity"),
			filepath.Join(home, "AppData", "Local", "Programs", "Google Antigravity IDE"),
			filepath.Join(home, "Applications", "Antigravity"),
		)
	}

	// Cover portable/custom installations such as D:\Antigravity without
	// recursively scanning entire disks.  Only enumerate mounted fixed drives:
	// probing every C:..Z: can block on disconnected network mappings or empty
	// removable media and used to make the desktop dashboard appear frozen.
	for _, root := range windowsFixedDriveRoots() {
		for _, relative := range []string{
			"Antigravity", "Antigravity IDE", "Antigravity 2", "Antigravity 2.0", "Antigravity Agent",
			"Google Antigravity", "Google Antigravity IDE",
			"Apps\\Antigravity", "Apps\\Antigravity IDE", "Apps\\Antigravity 2.0",
			"Programs\\Antigravity", "Programs\\Antigravity IDE", "Programs\\Antigravity 2.0",
			"Program Files\\Antigravity", "Program Files\\Antigravity IDE", "Program Files\\Antigravity 2.0",
		} {
			add(filepath.Join(root, relative))
		}
	}
	if includeShell {
		add(windowsShellDiscoveredPaths()...)
	}
	return candidates, false
}

// windowsSavedInstallPaths reuses only the non-secret installation paths
// recorded after a successful connection. The state file never contains
// accounts, model credentials, chat history or user configuration.
func windowsSavedInstallPaths() []string {
	stateName := "antigravity-install-state.json"
	if statePath := xiassPatcherInstallStatePath(stateName); statePath != "" {
		if data, err := os.ReadFile(statePath); err == nil {
			return windowsSavedInstallPathsFromData(data)
		}
	}
	// A standalone diagnostic can run before the app performs its storage
	// migration. Keep legacy state as a read-only fallback; normal XIASS Tools
	// startup has already migrated it to the path above.
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	for _, legacy := range []string{".antigravity-wf", ".antigravity-byok"} {
		data, readErr := os.ReadFile(filepath.Join(home, legacy, stateName))
		if readErr == nil {
			return windowsSavedInstallPathsFromData(data)
		}
	}
	return nil
}

func windowsSavedInstallPathsFromData(data []byte) []string {
	var state struct {
		Schema  int `json:"schema"`
		Targets []struct {
			AppPath string `json:"appPath"`
		} `json:"targets"`
	}
	if json.Unmarshal(data, &state) != nil || state.Schema != 1 {
		return nil
	}
	paths := make([]string, 0, len(state.Targets))
	seen := make(map[string]bool, len(state.Targets))
	for _, target := range state.Targets {
		path := strings.TrimSpace(target.AppPath)
		key := strings.ToLower(filepath.Clean(path))
		if path == "" || seen[key] {
			continue
		}
		seen[key] = true
		paths = append(paths, path)
	}
	return paths
}

// windowsFixedDriveRoots returns only currently mounted local fixed volumes.
// Explicit environment paths, registry locations and live process paths can
// still point to a removable or UNC install; they are intentionally not lost.
// The heuristic scan itself must not touch those potentially blocking paths.
func windowsFixedDriveRoots() []string {
	mask, err := winapi.GetLogicalDrives()
	if err != nil {
		return nil
	}
	return windowsDriveRootsForMask(mask, func(root string) uint32 {
		wide, conversionErr := winapi.UTF16PtrFromString(root)
		if conversionErr != nil {
			return winapi.DRIVE_UNKNOWN
		}
		return winapi.GetDriveType(wide)
	})
}

// windowsDriveRootsForMask is kept independent from the Win32 API so the
// safety rule (never probe absent/removable/remote letters) has a Windows CI
// regression test without relying on a particular runner's drives.
func windowsDriveRootsForMask(mask uint32, driveType func(string) uint32) []string {
	roots := make([]string, 0)
	for index := 0; index < 26; index++ {
		if mask&(uint32(1)<<uint(index)) == 0 {
			continue
		}
		root := fmt.Sprintf("%c:\\", 'A'+rune(index))
		if driveType(root) != winapi.DRIVE_FIXED {
			continue
		}
		roots = append(roots, root)
	}
	return roots
}

func windowsShellDiscoveredPaths() []string {
	path, err := exec.LookPath("powershell.exe")
	if err != nil {
		path, err = exec.LookPath("pwsh.exe")
		if err != nil {
			return nil
		}
	}
	const script = `[Console]::OutputEncoding=[Text.UTF8Encoding]::new(); $r=@(); ` +
		`Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object {$_.Name -like '*antigravity*.exe'} | ForEach-Object {$r += $_.ExecutablePath}; ` +
		`$u=@('HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*'); ` +
		`Get-ItemProperty $u -ErrorAction SilentlyContinue | Where-Object {$_.DisplayName -like '*Antigravity*'} | ForEach-Object {$r += $_.InstallLocation; $r += $_.DisplayIcon; $r += $_.UninstallString; $r += $_.QuietUninstallString}; ` +
		`$r | Where-Object {$_} | Sort-Object -Unique`
	ctx, cancel := context.WithTimeout(context.Background(), windowsShellDiscoveryTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	configureCommand(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	return windowsShellDiscoveryPaths(output)
}

func windowsShellDiscoveryPaths(output []byte) []string {
	paths := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r", ""), "\n") {
		if value := strings.TrimSpace(line); value != "" {
			paths = append(paths, value)
		}
	}
	return paths
}

func normalizeWindowsInstallRoot(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// DisplayIcon commonly stores a value like
	// `"C:\\Program Files\\Antigravity\\Antigravity.exe",0`.  The prior
	// `.exe,` truncation kept the leading quote, making os.Stat look for a
	// directory literally named `"C:` and miss a valid installation.  The same
	// parser also handles UninstallString's optional command-line arguments.
	if index := strings.LastIndex(strings.ToLower(value), ".exe"); index >= 0 {
		suffix := strings.TrimLeft(value[index+4:], " \t")
		if suffix == "" || strings.HasPrefix(suffix, `"`) || strings.HasPrefix(suffix, ",") ||
			strings.HasPrefix(suffix, "-") || strings.HasPrefix(suffix, "/") {
			value = value[:index+4]
		}
	}
	value = strings.Trim(strings.TrimSpace(value), `"`)
	value = filepath.Clean(value)
	lower := strings.ToLower(value)
	separator := string(os.PathSeparator)
	resourceToken := separator + "resources" + separator
	if index := strings.Index(lower, resourceToken); index >= 0 {
		value = value[:index]
	} else if strings.HasSuffix(lower, separator+"resources") {
		value = filepath.Dir(value)
	} else if strings.EqualFold(filepath.Ext(value), ".exe") {
		value = filepath.Dir(value)
	}
	abs, err := filepath.Abs(value)
	if err == nil {
		value = abs
	}
	return filepath.Clean(value)
}

func inspectWindowsInstall(root string) (windowsTarget, bool) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return windowsTarget{}, false
	}
	executable := locateWindowsExecutable(root)
	if executable == "" {
		return windowsTarget{}, false
	}
	resources := filepath.Join(root, "resources")
	unpacked := filepath.Join(resources, "app")
	mainPath := windowsExistingFile(filepath.Join(unpacked, "out", "main.js"))
	extensionRoot := filepath.Join(unpacked, "extensions", "antigravity")
	extensionEntry := windowsExistingFile(filepath.Join(extensionRoot, "dist", "extension.js"))
	language := locateWindowsLanguageServer(filepath.Join(extensionRoot, "bin"))
	if mainPath != "" && (extensionEntry != "" || language != "") {
		return windowsTarget{
			root: root, name: windowsProductName(executable, "Antigravity IDE"), kind: "ide",
			executable: executable, main: mainPath, extensionEntry: extensionEntry, language: language,
		}, true
	}

	asarPath := windowsExistingFile(filepath.Join(resources, "app.asar"))
	if asarPath == "" {
		return windowsTarget{}, false
	}
	for _, binDir := range []string{
		filepath.Join(resources, "bin"),
		filepath.Join(resources, "app.asar.unpacked", "bin"),
		filepath.Join(resources, "app", "extensions", "antigravity", "bin"),
	} {
		if language = locateWindowsLanguageServer(binDir); language != "" {
			break
		}
	}
	return windowsTarget{
		root: root, name: windowsProductName(executable, "Antigravity"), kind: "agent",
		executable: executable, asar: asarPath, language: language,
	}, true
}

func locateWindowsExecutable(root string) string {
	preferred := []string{
		filepath.Join(root, "Antigravity IDE.exe"), filepath.Join(root, "Antigravity.exe"),
		filepath.Join(root, "antigravity.exe"), filepath.Join(root, "Antigravity 2.exe"),
	}
	for _, path := range preferred {
		if windowsExistingFile(path) != "" {
			return path
		}
	}
	matches, _ := filepath.Glob(filepath.Join(root, "*.exe"))
	sort.Strings(matches)
	for _, path := range matches {
		name := strings.ToLower(filepath.Base(path))
		if strings.Contains(name, "antigravity") && !strings.Contains(name, "uninstall") &&
			!strings.Contains(name, "update") && !strings.Contains(name, "language_server") {
			return path
		}
	}
	return ""
}

func locateWindowsLanguageServer(binDir string) string {
	if binDir == "" {
		return ""
	}
	patterns := []string{"language_server_windows_x64.exe", "language_server.exe", "language_server*.exe"}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(filepath.Join(binDir, pattern))
		sort.Strings(matches)
		for _, path := range matches {
			if windowsExistingFile(path) != "" {
				return path
			}
		}
	}
	return ""
}

func windowsProductName(executable, fallback string) string {
	name := strings.TrimSuffix(filepath.Base(executable), filepath.Ext(executable))
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

func windowsTargetRank(target windowsTarget) int {
	lower := strings.ToLower(target.root)
	local := strings.ToLower(os.Getenv("LOCALAPPDATA"))
	if local != "" && strings.HasPrefix(lower, local) {
		return 0
	}
	if target.kind == "agent" {
		return 1
	}
	return 2
}
