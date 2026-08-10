package patcher

// Status holds the aggregate state used by the desktop UI.
type Status struct {
	AgentPatched     *bool
	IDEPatched       *bool
	IdeMainPatched   *bool
	ProxyListening   bool
	AsarPath         string
	LSPath           string
	IDEExtensionPath string
	IDELSPath        string
	Targets          []TargetStatus
}

// TargetStatus describes one detected Windows Antigravity installation.
type TargetStatus struct {
	Name               string
	Kind               string
	Version            string
	AppPath            string
	ExecutablePath     string
	MainPath           string
	ASARPath           string
	ExtensionPath      string
	LanguageServerPath string
	Supported          bool
	ConnectionMode     string
	Reason             string
	Patched            bool
}

// Run invokes the standalone native Windows patcher. No Python or external
// patch script is required at runtime.
func Run(action string) (string, error) {
	return runWindows(action)
}

func GetStatus() Status {
	return getWindowsStatus()
}

// MergeHistory restores sessions from older Antigravity data directories into
// the shared data directory. Existing files always win.
func MergeHistory() error {
	return mergeWindowsHistoryOnStartup()
}
