//go:build !linux && !windows && !darwin

package ports

import (
	"errors"
	"os"
)

// No reader exists for this OS: every identity is unknown, so every row is
// unverifiable and Kill refuses it.
func readIdentity(int) (uint64, error) { return 0, errors.ErrUnsupported }

func heldIdentity(*os.Process, int) (uint64, error) { return 0, errors.ErrUnsupported }

func isNoSuchProcess(error) bool { return false }
