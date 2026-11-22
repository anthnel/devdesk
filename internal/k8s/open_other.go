//go:build !unix

package k8s

// nonBlock is zero where there is no such flag; a FIFO is a Unix object.
const nonBlock = 0
