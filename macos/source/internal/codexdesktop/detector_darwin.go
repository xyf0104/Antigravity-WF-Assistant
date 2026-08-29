//go:build darwin

package codexdesktop

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	expectedBundleIdentifier = "com.openai.codex"
	maxInfoPlistSize         = 1 << 20
)

var publicVersionPattern = regexp.MustCompile(`\d+(?:\.\d+){0,3}`)

type macCandidate struct {
	bundle string
	source Source
}

type macInstallation struct {
	executable string
	source     Source
	version    string
}

type macInspection struct {
	installation           *macInstallation
	environmentUnavailable bool
	inspectionUnavailable  bool
	invalidInstallation    bool
}

// Discover checks only the four supported public application bundle paths.
// A bundle is accepted only when its public Info.plist declares
// com.openai.codex and its declared executable is a regular executable file.
func (detector *Detector) Discover(ctx context.Context) Status {
	status := Status{CheckedAt: detector.now()}
	inspection := inspectMacInstallation(detector.fileSystem())
	if inspection.installation == nil {
		if inspection.environmentUnavailable {
			addWarning(&status, WarningEnvironmentUnavailable)
		}
		if inspection.inspectionUnavailable {
			addWarning(&status, WarningInspectionUnavailable)
		}
		if inspection.invalidInstallation {
			addWarning(&status, WarningInvalidInstallation)
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
		Source:             inspection.installation.source,
		Version:            inspection.installation.version,
		ExecutableVerified: true,
	}

	if ctx == nil {
		ctx = context.Background()
	}
	processContext, cancel := context.WithTimeout(ctx, detector.processTimeout())
	defer cancel()
	processes, err := detector.processLister().List(processContext)
	if err != nil {
		addWarning(&status, WarningProcessListUnavailable)
		status.State = StateDegraded
		return status
	}

	for _, process := range processes {
		if sameMacPath(process.Executable, inspection.installation.executable) {
			status.Running = true
			status.State = StateRunning
			return status
		}
	}
	status.State = StateInstalled
	return status
}

func inspectMacInstallation(filesystem FileSystem) macInspection {
	inspection := macInspection{}
	candidates := []macCandidate{
		{bundle: "/Applications/Codex.app", source: SourceSystemApplications},
		{bundle: "/Applications/ChatGPT.app", source: SourceSystemApplications},
	}

	home, err := filesystem.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		inspection.environmentUnavailable = true
	} else {
		candidates = append(candidates,
			macCandidate{bundle: filepath.Join(home, "Applications", "Codex.app"), source: SourceUserApplications},
			macCandidate{bundle: filepath.Join(home, "Applications", "ChatGPT.app"), source: SourceUserApplications},
		)
	}

	for _, candidate := range candidates {
		installation, unavailable, invalid := inspectMacCandidate(filesystem, candidate)
		if installation != nil {
			inspection.installation = installation
			return inspection
		}
		inspection.inspectionUnavailable = inspection.inspectionUnavailable || unavailable
		inspection.invalidInstallation = inspection.invalidInstallation || invalid
	}
	return inspection
}

func inspectMacCandidate(filesystem FileSystem, candidate macCandidate) (*macInstallation, bool, bool) {
	bundleInfo, err := filesystem.Stat(candidate.bundle)
	if err != nil {
		if isNotExist(err) {
			return nil, false, false
		}
		return nil, true, false
	}
	if !bundleInfo.IsDir() {
		return nil, false, true
	}

	infoPath := filepath.Join(candidate.bundle, "Contents", "Info.plist")
	data, err := filesystem.ReadFile(infoPath)
	if err != nil {
		if isNotExist(err) {
			return nil, false, true
		}
		return nil, true, false
	}
	if len(data) > maxInfoPlistSize {
		return nil, false, true
	}
	metadata, ok := publicPlistStrings(data)
	if !ok || metadata["CFBundleIdentifier"] != expectedBundleIdentifier {
		return nil, false, true
	}

	executableName := strings.TrimSpace(metadata["CFBundleExecutable"])
	if !safeMacExecutableName(executableName) {
		return nil, false, true
	}
	executablePath := filepath.Join(candidate.bundle, "Contents", "MacOS", executableName)
	executableInfo, err := filesystem.Stat(executablePath)
	if err != nil {
		if isNotExist(err) {
			return nil, false, true
		}
		return nil, true, false
	}
	if !executableInfo.Mode().IsRegular() || executableInfo.Mode().Perm()&0111 == 0 {
		return nil, false, true
	}

	version := publicVersion(metadata["CFBundleShortVersionString"])
	if version == "" {
		version = publicVersion(metadata["CFBundleVersion"])
	}
	return &macInstallation{
		executable: executablePath,
		source:     candidate.source,
		version:    version,
	}, false, false
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

func safeMacExecutableName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsAny(name, "/\\\x00") && filepath.Base(name) == name
}

func sameMacPath(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func publicVersion(value string) string {
	return publicVersionPattern.FindString(strings.TrimSpace(value))
}

func publicPlistStrings(data []byte) (map[string]string, bool) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	values := make(map[string]string)
	pendingKey := ""
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return values, true
		}
		if err != nil {
			return nil, false
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			var key string
			if err := decoder.DecodeElement(&key, &start); err != nil {
				return nil, false
			}
			pendingKey = strings.TrimSpace(key)
		case "string":
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return nil, false
			}
			if pendingKey != "" {
				values[pendingKey] = strings.TrimSpace(value)
				pendingKey = ""
			}
		default:
			pendingKey = ""
		}
	}
}
