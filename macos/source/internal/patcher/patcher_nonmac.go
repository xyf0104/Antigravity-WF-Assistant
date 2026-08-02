//go:build !darwin

package patcher

import "fmt"

func runDarwin(_ string) (string, error) {
	return "", fmt.Errorf("macOS patcher is unavailable on this platform")
}

func getDarwinStatus() Status { return Status{} }
