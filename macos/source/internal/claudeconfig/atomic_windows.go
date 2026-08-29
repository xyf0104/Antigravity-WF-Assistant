//go:build windows

package claudeconfig

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

var (
	atomicKernel32  = syscall.NewLazyDLL("kernel32.dll")
	moveFileExWProc = atomicKernel32.NewProc("MoveFileExW")
)

// replaceFileAtomic uses MoveFileExW with write-through replacement because
// os.Rename cannot provide the same replacement guarantee on every supported
// Windows filesystem.
func replaceFileAtomic(source, destination string) error {
	sourcePointer, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPointer, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	result, _, callErr := moveFileExWProc.Call(
		uintptr(unsafe.Pointer(sourcePointer)),
		uintptr(unsafe.Pointer(destinationPointer)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if result == 0 {
		return fmt.Errorf("MoveFileExW: %w", callErr)
	}
	return nil
}
