package logging

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	setStdHandleProc = kernel32.NewProc("SetStdHandle")
)

const stdErrorHandle = ^uintptr(0) - 12 + 1 // STD_ERROR_HANDLE = -12

// redirectStderr points file descriptor 2 (stderr) at the given file.
// This captures Go runtime panics that write directly to stderr,
// which would otherwise vanish when built with -H windowsgui.
func redirectStderr(f *os.File) error {
	handle := syscall.Handle(f.Fd())
	r, _, err := setStdHandleProc.Call(stdErrorHandle, uintptr(unsafe.Pointer(handle)))
	if r == 0 {
		return err
	}
	os.Stderr = f
	return nil
}
