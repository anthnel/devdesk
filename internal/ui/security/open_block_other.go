//go:build !unix

package security

// openNoBlock is 0 where there is no such flag; the stat on the path and on the
// descriptor refuse anything that is not a regular file.
const openNoBlock = 0
