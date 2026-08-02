//go:build !(darwin || linux)

package store

import "os"

// flockExclusive is a no-op on platforms without syscall.Flock. automem only
// targets macOS and Linux (darwin || linux), where the real implementation
// lives; on other platforms the store's concurrency guarantee degrades to
// O_APPEND's line-atomicity, which still prevents interleaved lines but does
// not protect the rewrite path. Those platforms are explicitly unsupported.
func flockExclusive(f *os.File) error { return nil }
