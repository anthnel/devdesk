//go:build unix

package security

import "syscall"

// openNoBlock makes opening a FIFO that was swapped in after the stat return at
// once instead of waiting for a writer. A regular file is not affected.
const openNoBlock = syscall.O_NONBLOCK
