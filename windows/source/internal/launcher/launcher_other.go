//go:build !windows

package launcher

import (
	"fmt"
	"time"
)

func platformSupported() bool                            { return false }
func platformIsRunning(string) (bool, error)             { return false, nil }
func platformQuitGracefully(string, time.Duration) error { return fmt.Errorf("当前平台不支持") }
func platformLaunch(string) error                        { return fmt.Errorf("当前平台不支持") }
func platformWaitUntilRunning(string, time.Duration) error {
	return fmt.Errorf("当前平台不支持")
}
