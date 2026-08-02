package launcher

import "time"

// Supported reports whether the current platform supports safe lifecycle control.
func Supported() bool { return platformSupported() }

// IsRunning reports whether any process inside installRoot is active.
func IsRunning(installRoot string) (bool, error) { return platformIsRunning(installRoot) }

// QuitGracefully posts the normal close request to the selected installation
// and waits for all of its processes to exit. It never force-kills a process.
func QuitGracefully(installRoot string, timeout time.Duration) error {
	return platformQuitGracefully(installRoot, timeout)
}

// Launch starts the detected application executable.
func Launch(executablePath string) error { return platformLaunch(executablePath) }

// WaitUntilRunning waits for a process inside installRoot to appear.
func WaitUntilRunning(installRoot string, timeout time.Duration) error {
	return platformWaitUntilRunning(installRoot, timeout)
}
