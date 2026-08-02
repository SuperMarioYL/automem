//go:build darwin || linux

package store

import (
	"os"
	"syscall"
)

// flockExclusive acquires a blocking exclusive advisory flock on the lock
// file's descriptor, serializing concurrent writers across processes on the
// unix platforms automem targets (macOS + Linux). The lock is released when
// the fd is closed (see withLock's deferred Close), so a process that crashes
// mid-write can never leave the store stuck locked.
func flockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}
