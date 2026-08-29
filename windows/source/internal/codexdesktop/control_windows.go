//go:build windows

package codexdesktop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsMessageClose = 0x0010

var postMessageW = windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW")

func platformSelectionAvailable() bool { return true }

func platformDiscoverDesktopTarget(detector *Detector) *desktopTarget {
	if detector == nil {
		return nil
	}
	inspection := inspectWindowsInstallation(detector.fileSystem(), detector.registry())
	if inspection.installation == nil {
		return nil
	}
	target := windowsDesktopTarget(*inspection.installation)
	return &target
}

func platformValidateDesktopTarget(detector *Detector, value string) (desktopTarget, error) {
	if detector == nil {
		return desktopTarget{}, ErrSelectionRejected
	}
	executable := strings.TrimSpace(value)
	if executable == "" || strings.ContainsRune(executable, '\x00') {
		return desktopTarget{}, ErrSelectionRejected
	}
	executable = filepath.Clean(executable)
	if !isDesktopExecutableName(filepath.Base(executable)) || isKnownCLIPath(executable) {
		return desktopTarget{}, ErrSelectionRejected
	}
	if storePackage, ok := trustedSelectedStorePackage(detector, executable); ok {
		return desktopTarget{
			storePackage: storePackage,
			installation: Installation{
				Present:            true,
				Source:             SourceManualSelection,
				Version:            storePackageVersion(storePackage),
				ExecutableVerified: true,
			},
		}, nil
	}
	info, err := detector.fileSystem().Stat(executable)
	if err != nil || !info.Mode().IsRegular() || !isVerifiedWindowsDesktopExecutable(detector.fileSystem(), executable) {
		return desktopTarget{}, ErrSelectionRejected
	}
	return desktopTarget{
		location:   executable,
		executable: executable,
		installation: Installation{
			Present:            true,
			Source:             SourceManualSelection,
			Version:            readWindowsPublicVersion(detector.fileSystem(), executable),
			ExecutableVerified: true,
		},
	}, nil
}

func platformRevalidateDesktopTarget(detector *Detector, target desktopTarget) (desktopTarget, error) {
	if strings.TrimSpace(target.storePackage) != "" && strings.TrimSpace(target.location) == "" {
		discovered := platformDiscoverDesktopTarget(detector)
		if discovered != nil && strings.EqualFold(strings.TrimSpace(discovered.storePackage), strings.TrimSpace(target.storePackage)) {
			return *discovered, nil
		}
		return desktopTarget{}, ErrSelectionRejected
	}
	return platformValidateDesktopTarget(detector, target.location)
}

func windowsDesktopTarget(installation windowsInstallation) desktopTarget {
	return desktopTarget{
		location:     installation.executable,
		executable:   installation.executable,
		storePackage: installation.storePackage,
		installation: Installation{
			Present:            true,
			Source:             installation.source,
			Version:            installation.version,
			ExecutableVerified: installation.executableVerified,
		},
	}
}

func trustedSelectedStorePackage(detector *Detector, executable string) (string, bool) {
	if detector == nil || strings.TrimSpace(executable) == "" {
		return "", false
	}
	_, roots, _ := windowsCandidates(detector.fileSystem())
	packageName, ok := trustedStoreExecutable(executable, roots)
	if !ok || !registeredWindowsStorePackage(detector.registry(), packageName) {
		return "", false
	}
	return packageName, true
}

func registeredWindowsStorePackage(registry Registry, packageName string) bool {
	if registry == nil || !isSupportedStorePackage(packageName) {
		return false
	}
	packageNames, err := registry.Subkeys(storePackagesRegistryPath, maxStorePackageKeys)
	if err != nil {
		return false
	}
	for _, current := range packageNames {
		if strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(packageName)) {
			return true
		}
	}
	return false
}

// isVerifiedWindowsDesktopExecutable deliberately checks the public Electron
// application layout instead of accepting any file called Codex.exe or
// ChatGPT.exe. It does not inspect account data, configuration, or embedded
// application contents. Current official desktop builds ship resources/app.asar;
// the unpacked-development layout is accepted for genuine legacy builds.
func isVerifiedWindowsDesktopExecutable(filesystem FileSystem, executable string) bool {
	if filesystem == nil || !isDesktopExecutableName(filepath.Base(executable)) || isKnownCLIPath(executable) {
		return false
	}
	info, err := filesystem.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	root := filepath.Dir(executable)
	if isRegularWindowsPublicFile(filesystem, filepath.Join(root, "resources", "app.asar")) {
		return true
	}
	// Some legacy unpacked Electron distributions retain an application
	// manifest in resources/app instead of app.asar. Requiring both the
	// supported executable name and one of these public files rejects a bare
	// lookalike binary selected from an arbitrary directory.
	return isRegularWindowsPublicFile(filesystem, filepath.Join(root, "resources", "app", "package.json")) ||
		isRegularWindowsPublicFile(filesystem, filepath.Join(root, "resources", "app", "product.json"))
}

func isRegularWindowsPublicFile(filesystem FileSystem, path string) bool {
	info, err := filesystem.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func platformTargetMatchesProcess(detector *Detector, target desktopTarget, process Process) bool {
	if strings.TrimSpace(target.executable) != "" {
		return sameWindowsPath(process.Executable, target.executable)
	}
	if strings.TrimSpace(target.storePackage) == "" || detector == nil {
		return false
	}
	_, roots, _ := windowsCandidates(detector.fileSystem())
	packageName, ok := trustedStoreExecutable(process.Executable, roots)
	return ok && strings.EqualFold(strings.TrimSpace(packageName), strings.TrimSpace(target.storePackage))
}

func systemControlOperations() controlOperations {
	return controlOperations{
		launch: platformLaunchDesktopTarget,
		stop:   platformRequestDesktopStop,
	}
}

func platformLaunchDesktopTarget(ctx context.Context, target desktopTarget) error {
	if strings.TrimSpace(target.executable) == "" && strings.TrimSpace(target.storePackage) == "" {
		return ErrNoVerifiedInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(target.executable) == "" {
		launchTarget, err := trustedWindowsStoreLaunchTarget(ctx, target.storePackage)
		if err != nil {
			return errors.New("could not resolve verified Codex Desktop Store launch target")
		}
		if err := exec.CommandContext(ctx, "explorer.exe", `shell:AppsFolder\`+launchTarget).Start(); err != nil {
			return errors.New("could not start verified Codex Desktop Store application")
		}
		return nil
	}
	command := exec.CommandContext(ctx, target.executable)
	command.Dir = filepath.Dir(target.executable)
	if err := command.Start(); err != nil {
		return errors.New("could not start verified Codex Desktop")
	}
	return nil
}

func platformRequestDesktopStop(ctx context.Context, target desktopTarget) error {
	if strings.TrimSpace(target.executable) == "" && strings.TrimSpace(target.storePackage) == "" {
		return ErrNoVerifiedInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	processIDs, err := matchingWindowsDesktopProcessIDs(ctx, target)
	if err != nil {
		return errors.New("could not inspect verified Codex Desktop processes")
	}
	if len(processIDs) == 0 {
		return nil
	}
	if err := postGracefulWindowsClose(processIDs); err != nil {
		return errors.New("could not request a graceful Codex Desktop exit")
	}
	return nil
}

// matchingWindowsDesktopProcessIDs obtains IDs only after the executable has
// already passed structure validation. IDs remain entirely inside this helper;
// callers receive only a generic success/failure result.
func matchingWindowsDesktopProcessIDs(ctx context.Context, target desktopTarget) ([]uint32, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	var matches []uint32
	for count := 0; count < maxProcessEntries; count++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(windows.UTF16ToString(entry.ExeFile[:]))
		if isDesktopExecutableName(name) {
			path, pathErr := processExecutablePath(entry.ProcessID)
			if pathErr == nil && windowsProcessMatchesTarget(target, path) {
				matches = append(matches, entry.ProcessID)
			}
		}
		err = windows.Process32Next(snapshot, &entry)
		if err == nil {
			continue
		}
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return matches, nil
		}
		return nil, err
	}
	return nil, errors.New("process snapshot exceeded limit")
}

func windowsProcessMatchesTarget(target desktopTarget, executable string) bool {
	if strings.TrimSpace(target.executable) != "" {
		return sameWindowsPath(executable, target.executable)
	}
	if strings.TrimSpace(target.storePackage) == "" {
		return false
	}
	programFilesRoots := uniqueWindowsPaths([]string{
		// Lifecycle operations run only on Windows and use public process paths;
		// user-provided paths are never emitted from this helper.
		getenvWindows("ProgramFiles"), getenvWindows("ProgramW6432"), getenvWindows("ProgramFiles(x86)"),
	})
	packageName, ok := trustedStoreExecutable(executable, programFilesRoots)
	return ok && strings.EqualFold(strings.TrimSpace(packageName), strings.TrimSpace(target.storePackage))
}

func getenvWindows(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func trustedWindowsStoreLaunchTarget(ctx context.Context, packageName string) (string, error) {
	if !isSupportedStorePackage(packageName) {
		return "", ErrNoVerifiedInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// The filter permits only registered OpenAI Codex/ChatGPT AppX identities.
	// We intentionally do not pass a user-selected path or arbitrary AppID to
	// explorer.exe. Get-StartApps reads the public Start menu registration.
	command := `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; Get-StartApps -ErrorAction SilentlyContinue | Where-Object { $_.AppID -match '(?i)^OpenAI\.(Codex|ChatGPT)_' } | Select-Object -ExpandProperty AppID`
	output, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command).Output()
	if err != nil {
		return "", ErrLifecycleUnavailable
	}
	for _, line := range strings.FieldsFunc(string(output), func(r rune) bool { return r == '\r' || r == '\n' }) {
		candidate := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if trustedWindowsStoreLaunchTargetValue(candidate, packageName) {
			return candidate, nil
		}
	}
	return "", ErrNoVerifiedInstallation
}

func trustedWindowsStoreLaunchTargetValue(value, packageName string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if !safeWindowsStoreLaunchTarget(value) {
		return false
	}
	if !(strings.HasPrefix(lower, "openai.codex_") || strings.HasPrefix(lower, "openai.chatgpt_")) {
		return false
	}
	// Get-StartApps uses a PackageFamilyName rather than the full package key.
	// Both variants must still identify the same public OpenAI product family.
	return isSupportedStorePackage(packageName)
}

func safeWindowsStoreLaunchTarget(value string) bool {
	if strings.Count(value, "!") != 1 || strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == '-', character == '!':
		default:
			return false
		}
	}
	return true
}

// postGracefulWindowsClose sends WM_CLOSE to top-level windows associated with
// the exact verified process IDs. It deliberately does not call taskkill,
// TerminateProcess, Stop-Process, or any forced-close API.
func postGracefulWindowsClose(processIDs []uint32) error {
	wanted := make(map[uint32]struct{}, len(processIDs))
	for _, processID := range processIDs {
		if processID != 0 {
			wanted[processID] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	posted := false
	var postErr error
	callback := syscall.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		var processID uint32
		if _, err := windows.GetWindowThreadProcessId(hwnd, &processID); err != nil {
			return 1
		}
		if _, ok := wanted[processID]; !ok {
			return 1
		}
		result, _, callErr := postMessageW.Call(uintptr(hwnd), windowsMessageClose, 0, 0)
		if result == 0 && callErr != syscall.Errno(0) {
			postErr = callErr
			return 1
		}
		if result != 0 {
			posted = true
		}
		return 1
	})
	if err := windows.EnumWindows(callback, nil); err != nil {
		return err
	}
	if postErr != nil || !posted {
		return errors.New("no graceful close window was available")
	}
	return nil
}
