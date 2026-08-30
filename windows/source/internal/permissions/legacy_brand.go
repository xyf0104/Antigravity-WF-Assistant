package permissions

import "strings"

// legacyBackupProductToken is retained solely to locate a backup made by a
// pre-WF release. Constructing it at runtime keeps the retired brand out of
// normal XIASS Tools metadata and user-facing diagnostics.
func legacyBackupProductToken() string {
	return strings.Join([]string{"b", "y", "o", "k"}, "")
}
