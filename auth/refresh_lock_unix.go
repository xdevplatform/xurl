//go:build darwin || linux

package auth

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryRefreshLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
		return false, nil
	}
	return err == nil, err
}

func releaseRefreshLock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
