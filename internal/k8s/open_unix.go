//go:build unix

package k8s

import "syscall"

// nonBlock is the open flag that keeps a FIFO from blocking until a writer
// shows up.
const nonBlock = syscall.O_NONBLOCK
