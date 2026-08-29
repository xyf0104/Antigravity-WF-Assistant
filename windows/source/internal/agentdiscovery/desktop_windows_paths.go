package agentdiscovery

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"antigravity-byok/internal/agent"
)

type desktopSpec struct {
	id             agent.ID
	displayName    string
	executableName string
	installNames   []string
	dataNames      []string
}

var (
	cursorSpec = desktopSpec{
		id:             agent.CursorID,
		displayName:    "Cursor",
		executableName: "Cursor.exe",
		installNames:   []string{"Cursor"},
		dataNames:      []string{"Cursor"},
	}
	windsurfSpec = desktopSpec{
		id:             agent.WindsurfID,
		displayName:    "Windsurf",
		executableName: "Windsurf.exe",
		installNames:   []string{"Windsurf"},
		dataNames:      []string{"Windsurf"},
	}
)

func joinClaudeConfigPath(home string) string {
	return filepath.Join(home, ".claude")
}

func pathDirectory(path string) string {
	return filepath.Dir(path)
}

func (adapter *Adapter) detectDesktop(spec desktopSpec) agent.Status {
	filesystem := adapter.fileSystem()
	candidates, environmentIssue := windowsDesktopCandidates(filesystem, spec)
	installRoot, executablePath, installIssue := findWindowsInstallation(filesystem, candidates.installDirectories, candidates.installExecutables)
	dataPath, dataIssue := findFirstDirectory(filesystem, candidates.dataDirectories)

	issues := make([]string, 0, 3)
	if environmentIssue != nil {
		issues = append(issues, environmentIssue.Error())
	}
	if installIssue != nil {
		issues = append(issues, "an installation candidate could not be inspected")
	}
	if dataIssue != nil {
		issues = append(issues, "a local data directory could not be inspected")
	}
	if installRoot != "" && executablePath == "" {
		issues = append(issues, "the installed application executable is missing")
	}

	installFound := installRoot != ""
	dataFound := dataPath != ""
	if !installFound && !dataFound {
		if len(issues) > 0 {
			return adapter.newStatus(agent.StateDegraded, spec.displayName+" discovery is incomplete: "+strings.Join(issues, "; ")+".")
		}
		return adapter.newStatus(agent.StateNotInstalled, spec.displayName+" was not found in supported installation or local-data locations.")
	}

	version := ""
	if executablePath != "" {
		// Product version is useful when the installation exposes it, but it is
		// never a prerequisite for local discovery and never read from user data.
		version, _ = adapter.versionReader()(filesystem, executablePath)
	}

	state := agent.StateDetected
	message := spec.displayName + " was partially detected."
	if installFound && executablePath != "" && dataFound {
		state = agent.StateReady
		message = spec.displayName + " installation and local data directory were found."
	}
	if len(issues) > 0 {
		state = agent.StateDegraded
		message = spec.displayName + " was found, but discovery is incomplete: " + strings.Join(issues, "; ") + "."
	}

	status := adapter.newStatus(state, message)
	status.Installation.Root = installRoot
	status.Installation.ExecutablePath = executablePath
	status.Installation.Version = version
	status.Installation.Platform = "windows"
	if !installFound {
		status.Installation.Root = dataPath
	}
	if dataFound {
		status.Details = map[string]string{"dataDirectory": dataPath}
	}
	return status
}

type desktopCandidates struct {
	installDirectories []string
	installExecutables []string
	dataDirectories    []string
}

func windowsDesktopCandidates(filesystem FileSystem, spec desktopSpec) (desktopCandidates, error) {
	localAppData := strings.TrimSpace(filesystem.Getenv("LOCALAPPDATA"))
	appData := strings.TrimSpace(filesystem.Getenv("APPDATA"))
	programFiles := strings.TrimSpace(filesystem.Getenv("ProgramFiles"))
	programFilesX86 := strings.TrimSpace(filesystem.Getenv("ProgramFiles(x86)"))
	home, homeErr := filesystem.UserHomeDir()
	if localAppData == "" && strings.TrimSpace(home) != "" {
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	if appData == "" && strings.TrimSpace(home) != "" {
		appData = filepath.Join(home, "AppData", "Roaming")
	}

	candidates := desktopCandidates{}
	for _, installName := range spec.installNames {
		for _, root := range []string{localAppData, programFiles, programFilesX86} {
			if root == "" {
				continue
			}
			for _, installRoot := range []string{
				filepath.Join(root, "Programs", installName),
				filepath.Join(root, installName),
			} {
				candidates.installDirectories = append(candidates.installDirectories, installRoot)
				candidates.installExecutables = append(candidates.installExecutables, filepath.Join(installRoot, spec.executableName))
			}
		}
	}
	for _, dataName := range spec.dataNames {
		for _, root := range []string{appData, localAppData} {
			if root != "" {
				candidates.dataDirectories = append(candidates.dataDirectories, filepath.Join(root, dataName))
			}
		}
	}
	candidates.installDirectories = uniqueNonEmpty(candidates.installDirectories)
	candidates.installExecutables = uniqueNonEmpty(candidates.installExecutables)
	candidates.dataDirectories = uniqueNonEmpty(candidates.dataDirectories)
	if len(candidates.installExecutables) == 0 && len(candidates.dataDirectories) == 0 {
		if homeErr != nil {
			return candidates, errors.New("the Windows user directories could not be resolved")
		}
		return candidates, errors.New("the Windows application directories are unavailable")
	}
	return candidates, nil
}

func findWindowsInstallation(filesystem FileSystem, roots, executables []string) (string, string, error) {
	var firstErr error
	for _, candidate := range executables {
		present, err := existingRegularFile(filesystem, candidate)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if present {
			return filepath.Dir(candidate), candidate, nil
		}
	}
	for _, root := range roots {
		present, err := existingDirectory(filesystem, root)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if present {
			return root, "", firstErr
		}
	}
	return "", "", firstErr
}

func findFirstDirectory(filesystem FileSystem, candidates []string) (string, error) {
	var firstErr error
	for _, candidate := range candidates {
		present, err := existingDirectory(filesystem, candidate)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if present {
			return candidate, nil
		}
	}
	return "", firstErr
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.Clean(strings.TrimSpace(value))
		if value == "" || value == "." {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

// readDesktopProductVersion reads only public package metadata colocated with
// a desktop executable. A missing or malformed metadata file simply leaves
// the version empty; discovery does not infer an application version from user
// profiles, workspace data, or credentials.
func readDesktopProductVersion(filesystem FileSystem, executablePath string) (string, error) {
	root := filepath.Dir(executablePath)
	for _, candidate := range []string{
		filepath.Join(root, "resources", "app", "product.json"),
		filepath.Join(root, "resources", "app", "package.json"),
	} {
		data, err := filesystem.ReadFile(candidate)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			continue
		}
		var metadata struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			continue
		}
		if version := parseVersion(metadata.Version); version != "" {
			return version, nil
		}
	}
	return "", nil
}
