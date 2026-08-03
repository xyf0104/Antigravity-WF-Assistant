//go:build windows

package updater

import "os/exec"

// LaunchInstaller starts the verified NSIS installer. It intentionally leaves
// the installer interactive so Windows can show its normal elevation and
// desktop-shortcut choices.
func LaunchInstaller(path string) error {
	return exec.Command(path).Start()
}
