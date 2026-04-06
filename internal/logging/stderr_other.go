//go:build !windows

package logging

import (
	"os"
	"syscall"
)

// redirectStderr points file descriptor 2 (stderr) at the given file.
func redirectStderr(f *os.File) error {
	return syscall.Dup2(int(f.Fd()), 2)
}
