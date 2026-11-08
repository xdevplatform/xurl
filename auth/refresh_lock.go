package auth

import (
	"fmt"
	"os"
	"time"
)

// The sidecar inode stays stable while the token file is replaced atomically.
func lockRefreshStore(path string) (func(), error) {
	if path == "" {
		return nil, fmt.Errorf("token store has no path")
	}
	f, err := os.OpenFile(path+".refresh.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open refresh lock: %w", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		acquired, err := tryRefreshLock(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("acquire refresh lock: %w", err)
		}
		if acquired {
			return func() { _ = releaseRefreshLock(f); _ = f.Close() }, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("refresh lock timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
