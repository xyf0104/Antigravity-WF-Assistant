//go:build windows

package codexdesktop

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	storePackagesRegistryPath = `Software\Classes\ActivatableClasses\Package`
	maxStorePackageKeys       = 2048
	maxAppPathValues          = 6
	maxPublicMetadataSize     = 1 << 20
)

var publicVersionPattern = regexp.MustCompile(`\d+(?:\.\d+){0,3}`)

type windowsCandidate struct {
	executable string
	source     Source
}

type windowsInstallation struct {
	executable         string
	storePackage       string
	source             Source
	version            string
	executableVerified bool
}

type windowsInspection struct {
	installation           *windowsInstallation
	verifiedInstallations  []windowsInstallation
	programFilesRoots      []string
	environmentUnavailable bool
	inspectionUnavailable  bool
	invalidInstallation    bool
}

// appPathRegistry is intentionally optional because Store package discovery
// needs only Registry.Subkeys. Windows production supplies this narrow,
// read-only public App Paths reader; tests or other callers that implement
// only Registry retain the existing fixed-location and Store behavior.
//
// Values are used only in memory and are structure-validated before they can
// influence a detected installation. They are never copied into Status,
// diagnostics, logs, or an error returned to the renderer.
type appPathRegistry interface {
	AppPathValues(limit int) ([]string, error)
}

// Discover observes only fixed public installation locations, six fixed public
// App Paths registrations, public Store package registration, and a bounded
// Toolhelp process snapshot. It never searches PATH, reads profile data, or
// treats an arbitrary codex.exe name as proof of a desktop installation.
func (detector *Detector) Discover(ctx context.Context) Status {
	status := Status{CheckedAt: detector.now()}
	inspection := inspectWindowsInstallation(detector.fileSystem(), detector.registry())

	if ctx == nil {
		ctx = context.Background()
	}
	processContext, cancel := context.WithTimeout(ctx, detector.processTimeout())
	defer cancel()
	processes, processErr := detector.processLister().List(processContext)
	if processErr == nil && processContext.Err() != nil {
		processErr = processContext.Err()
	}

	var runningInstallation *windowsInstallation
	if processErr == nil {
		runningInstallation = findRunningWindowsInstallation(detector.fileSystem(), processes, inspection.verifiedInstallations, inspection.programFilesRoots)
	}

	installation := inspection.installation
	// Store registration proves only the package identity; it does not expose a
	// stable executable path. When the bounded process snapshot proves that
	// the *same registered Store package* is running, promote that matching
	// runtime record so Status accurately reports executable verification. Do
	// not promote a different Store package or another fixed/App Paths target:
	// those must remain deterministic lifecycle targets.
	if installation != nil && runningInstallation != nil && sameWindowsStorePackage(*installation, *runningInstallation) {
		installation = runningInstallation
	}
	if installation == nil && runningInstallation != nil {
		// Retain a deterministic discovery target whenever one exists. A
		// separately running verified app must still make the status Running,
		// but it must not silently replace the lifecycle target selected from
		// bounded public installation sources.
		installation = runningInstallation
	}
	if installation == nil {
		if inspection.environmentUnavailable {
			addWarning(&status, WarningEnvironmentUnavailable)
		}
		if inspection.inspectionUnavailable {
			addWarning(&status, WarningInspectionUnavailable)
		}
		if inspection.invalidInstallation {
			addWarning(&status, WarningInvalidInstallation)
		}
		if processErr != nil {
			addWarning(&status, WarningProcessListUnavailable)
		}
		if len(status.Warnings) == 0 {
			status.State = StateNotInstalled
		} else {
			status.State = StateDegraded
		}
		return status
	}

	status.Installation = Installation{
		Present:            true,
		Source:             installation.source,
		Version:            installation.version,
		ExecutableVerified: installation.executableVerified,
	}
	if processErr != nil {
		addWarning(&status, WarningProcessListUnavailable)
		status.State = StateDegraded
		return status
	}
	if runningInstallation != nil {
		status.Running = true
		status.State = StateRunning
		return status
	}
	status.State = StateInstalled
	return status
}

func (detector *Detector) registry() Registry {
	if detector != nil && detector.options.Registry != nil {
		return detector.options.Registry
	}
	return systemRegistry{}
}

func inspectWindowsInstallation(filesystem FileSystem, registry Registry) windowsInspection {
	candidates, programFilesRoots, environmentUnavailable := windowsCandidates(filesystem)
	inspection := windowsInspection{
		programFilesRoots:      programFilesRoots,
		environmentUnavailable: environmentUnavailable,
	}
	for _, candidate := range candidates {
		installation, unavailable, invalid := inspectWindowsCandidate(filesystem, candidate)
		if installation != nil {
			inspection.addInstallation(installation)
			continue
		}
		inspection.inspectionUnavailable = inspection.inspectionUnavailable || unavailable
		inspection.invalidInstallation = inspection.invalidInstallation || invalid
	}

	appPathInstallations, unavailable, invalid := inspectWindowsAppPaths(filesystem, registry)
	for index := range appPathInstallations {
		inspection.addInstallation(&appPathInstallations[index])
	}
	inspection.inspectionUnavailable = inspection.inspectionUnavailable || unavailable
	inspection.invalidInstallation = inspection.invalidInstallation || invalid

	storeInstallation, unavailable := inspectWindowsStore(registry)
	if storeInstallation != nil {
		inspection.addInstallation(storeInstallation)
	}
	inspection.inspectionUnavailable = inspection.inspectionUnavailable || unavailable
	return inspection
}

func (inspection *windowsInspection) addInstallation(installation *windowsInstallation) {
	if inspection == nil || installation == nil {
		return
	}
	if inspection.installation == nil {
		copy := *installation
		inspection.installation = &copy
	}
	for _, current := range inspection.verifiedInstallations {
		if sameWindowsInstallation(current, *installation) {
			return
		}
	}
	inspection.verifiedInstallations = append(inspection.verifiedInstallations, *installation)
}

func sameWindowsInstallation(left, right windowsInstallation) bool {
	if left.executable != "" || right.executable != "" {
		return sameWindowsPath(left.executable, right.executable)
	}
	return left.storePackage != "" && strings.EqualFold(strings.TrimSpace(left.storePackage), strings.TrimSpace(right.storePackage))
}

func sameWindowsStorePackage(left, right windowsInstallation) bool {
	return left.source == SourceWindowsStore &&
		right.source == SourceWindowsStore &&
		strings.TrimSpace(left.storePackage) != "" &&
		strings.TrimSpace(right.storePackage) != "" &&
		strings.EqualFold(strings.TrimSpace(left.storePackage), strings.TrimSpace(right.storePackage))
}

func inspectWindowsAppPaths(filesystem FileSystem, registry Registry) ([]windowsInstallation, bool, bool) {
	reader, ok := registry.(appPathRegistry)
	if !ok || reader == nil {
		return nil, false, false
	}
	values, err := reader.AppPathValues(maxAppPathValues)
	unavailable := err != nil
	invalid := false
	installations := make([]windowsInstallation, 0, len(values))
	for _, value := range values {
		executable := normalizeWindowsAppPathValue(value)
		if executable == "" {
			invalid = true
			continue
		}
		installation, candidateUnavailable, candidateInvalid := inspectWindowsCandidate(filesystem, windowsCandidate{
			executable: executable,
			source:     SourcePublicDiscovery,
		})
		if installation != nil {
			installations = append(installations, *installation)
			continue
		}
		unavailable = unavailable || candidateUnavailable
		invalid = invalid || candidateInvalid
	}
	return installations, unavailable, invalid
}

// normalizeWindowsAppPathValue accepts only a single absolute executable path
// from a public App Paths default value. It deliberately rejects arguments,
// newlines, environment-variable expansion, and arbitrary executable names;
// the value cannot become a command line or a generic Codex CLI fallback.
func normalizeWindowsAppPathValue(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "\ufeff"))
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	if strings.HasPrefix(value, `"`) {
		closingQuote := strings.Index(value[1:], `"`)
		if closingQuote < 0 {
			return ""
		}
		closingQuote++
		if strings.TrimSpace(value[closingQuote+1:]) != "" {
			return ""
		}
		value = value[1:closingQuote]
	} else if strings.ContainsRune(value, '"') {
		return ""
	}
	if !filepath.IsAbs(value) {
		return ""
	}
	value = filepath.Clean(value)
	if !isDesktopExecutableName(filepath.Base(value)) || isKnownCLIPath(value) {
		return ""
	}
	return value
}

func windowsCandidates(filesystem FileSystem) ([]windowsCandidate, []string, bool) {
	localAppData := strings.TrimSpace(filesystem.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		home, err := filesystem.UserHomeDir()
		if err == nil && strings.TrimSpace(home) != "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
	}

	programFilesRoots := uniqueWindowsPaths([]string{
		filesystem.Getenv("ProgramFiles"),
		filesystem.Getenv("ProgramW6432"),
		filesystem.Getenv("ProgramFiles(x86)"),
	})
	candidates := make([]windowsCandidate, 0, 6)
	if localAppData != "" {
		candidates = append(candidates,
			windowsCandidate{executable: filepath.Join(localAppData, "Programs", "Codex", "Codex.exe"), source: SourceLocalAppData},
			windowsCandidate{executable: filepath.Join(localAppData, "Programs", "ChatGPT", "ChatGPT.exe"), source: SourceLocalAppData},
		)
	}
	for _, root := range programFilesRoots {
		candidates = append(candidates,
			windowsCandidate{executable: filepath.Join(root, "Codex", "Codex.exe"), source: SourceProgramFiles},
			windowsCandidate{executable: filepath.Join(root, "ChatGPT", "ChatGPT.exe"), source: SourceProgramFiles},
		)
	}
	return uniqueWindowsCandidates(candidates), programFilesRoots, localAppData == "" && len(programFilesRoots) == 0
}

func inspectWindowsCandidate(filesystem FileSystem, candidate windowsCandidate) (*windowsInstallation, bool, bool) {
	info, err := filesystem.Stat(candidate.executable)
	if err != nil {
		if isNotExist(err) {
			return nil, false, false
		}
		return nil, true, false
	}
	if !info.Mode().IsRegular() || isKnownCLIPath(candidate.executable) || !isVerifiedWindowsDesktopExecutable(filesystem, candidate.executable) {
		return nil, false, true
	}
	return &windowsInstallation{
		executable:         candidate.executable,
		source:             candidate.source,
		version:            readWindowsPublicVersion(filesystem, candidate.executable),
		executableVerified: true,
	}, false, false
}

func inspectWindowsStore(registry Registry) (*windowsInstallation, bool) {
	if registry == nil {
		return nil, false
	}
	packageNames, err := registry.Subkeys(storePackagesRegistryPath, maxStorePackageKeys)
	if err != nil {
		if isNotExist(err) {
			return nil, false
		}
		return nil, true
	}
	for _, packageName := range packageNames {
		if !isSupportedStorePackage(packageName) {
			continue
		}
		return &windowsInstallation{
			storePackage: packageName,
			source:       SourceWindowsStore,
			version:      storePackageVersion(packageName),
		}, false
	}
	return nil, false
}

func findRunningWindowsInstallation(filesystem FileSystem, processes []Process, known []windowsInstallation, programFilesRoots []string) *windowsInstallation {
	for index, process := range processes {
		if index >= maxProcessEntries {
			break
		}
		executable := strings.TrimSpace(process.Executable)
		if executable == "" || !isDesktopExecutableName(filepath.Base(executable)) || isKnownCLIPath(executable) {
			continue
		}
		for knownIndex := range known {
			installation := &known[knownIndex]
			if installation.executable != "" && sameWindowsPath(executable, installation.executable) {
				return installation
			}
		}
		if packageName, ok := trustedStoreExecutable(executable, programFilesRoots); ok {
			return &windowsInstallation{
				executable:         executable,
				storePackage:       packageName,
				source:             SourceWindowsStore,
				version:            storePackageVersion(packageName),
				executableVerified: true,
			}
		}
		if installation := inspectRunningWindowsExecutable(filesystem, executable); installation != nil {
			return installation
		}
	}
	return nil
}

// inspectRunningWindowsExecutable is a narrow fallback for a live Codex or
// ChatGPT Desktop process that is not present in one of the bounded public
// installation registries. It never accepts a path by name alone: it requires
// the same regular-file, known-CLI exclusion, and public Electron layout
// checks used for static discovery. The process path is kept only in memory.
func inspectRunningWindowsExecutable(filesystem FileSystem, executable string) *windowsInstallation {
	executable = strings.TrimSpace(executable)
	if filesystem == nil || executable == "" || !isDesktopExecutableName(filepath.Base(executable)) || isKnownCLIPath(executable) {
		return nil
	}
	info, err := filesystem.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || !isVerifiedWindowsDesktopExecutable(filesystem, executable) {
		return nil
	}
	return &windowsInstallation{
		executable:         executable,
		source:             SourcePublicDiscovery,
		version:            readWindowsPublicVersion(filesystem, executable),
		executableVerified: true,
	}
}

func readWindowsPublicVersion(filesystem FileSystem, executable string) string {
	root := filepath.Dir(executable)
	for _, metadataPath := range []string{
		filepath.Join(root, "resources", "app", "product.json"),
		filepath.Join(root, "resources", "app", "package.json"),
	} {
		data, err := filesystem.ReadFile(metadataPath)
		if err != nil || len(data) > maxPublicMetadataSize {
			continue
		}
		var metadata struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &metadata); err == nil {
			if version := publicVersion(metadata.Version); version != "" {
				return version
			}
		}
	}
	return ""
}

func trustedStoreExecutable(executable string, programFilesRoots []string) (string, bool) {
	if !isDesktopExecutableName(filepath.Base(executable)) {
		return "", false
	}
	for _, programFiles := range programFilesRoots {
		windowsApps := filepath.Join(programFiles, "WindowsApps")
		relative, err := filepath.Rel(windowsApps, executable)
		if err != nil || !pathInside(relative) {
			continue
		}
		parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
		if len(parts) < 2 || !isSupportedStorePackage(parts[0]) {
			continue
		}
		return parts[0], true
	}
	return "", false
}

func isDesktopExecutableName(name string) bool {
	return strings.EqualFold(name, "Codex.exe") || strings.EqualFold(name, "ChatGPT.exe")
}

func isSupportedStorePackage(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(name, "openai.codex_") || strings.HasPrefix(name, "openai.chatgpt_")
}

func storePackageVersion(name string) string {
	parts := strings.Split(strings.TrimSpace(name), "_")
	if len(parts) < 2 {
		return ""
	}
	return publicVersion(parts[1])
}

func publicVersion(value string) string {
	return publicVersionPattern.FindString(strings.TrimSpace(value))
}

func isKnownCLIPath(path string) bool {
	normalized := "/" + strings.Trim(strings.ToLower(strings.ReplaceAll(filepath.Clean(strings.TrimSpace(path)), "\\", "/")), "/") + "/"
	for _, blocked := range []string{
		"/node_modules/",
		"/appdata/roaming/npm/",
		"/appdata/local/npm/",
		"/.cargo/bin/",
		"/cargo/bin/",
		"/scoop/apps/",
		"/chocolatey/bin/",
		"/chocolatey/lib/",
		"/appdata/local/antigravity/",
		"/appdata/roaming/antigravity/",
	} {
		if strings.Contains(normalized, blocked) {
			return true
		}
	}
	return false
}

func sameWindowsPath(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	return left != "" && right != "" && strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func pathInside(path string) bool {
	if path == "." || path == ".." || filepath.IsAbs(path) {
		return false
	}
	return !strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func uniqueWindowsPaths(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	paths := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		value = filepath.Clean(value)
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		paths = append(paths, value)
	}
	return paths
}

func uniqueWindowsCandidates(candidates []windowsCandidate) []windowsCandidate {
	seen := make(map[string]struct{}, len(candidates))
	unique := make([]windowsCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.executable = filepath.Clean(strings.TrimSpace(candidate.executable))
		if candidate.executable == "" || candidate.executable == "." {
			continue
		}
		key := strings.ToLower(candidate.executable)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
