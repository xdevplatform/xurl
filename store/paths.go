package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// Names of the files inside the ~/.xurl directory.
const (
	authFileName = "auth.yml"
	keysFileName = "keys.yml"
)

// resolveStoreDir returns ~/.xurl as a directory, creating it if needed and
// migrating the legacy single-file layout on first use:
//
//	~/.xurl (file) -> ~/.xurl/auth.yml  (tokens and app credentials)
//
// Migration is rename-based (atomic on the same filesystem) and
// non-destructive: if any step fails, the legacy file is left (or restored)
// where it was and the legacy path is returned so the caller keeps working
// against the old layout.
func resolveStoreDir() string {
	homeDir := resolveHomeDir()
	root := filepath.Join(homeDir, ".xurl")
	tmp := root + ".migrating"

	// Recover from a migration interrupted between its two renames: the
	// legacy file is stranded at the temp path and root is missing or an
	// empty directory. Finish moving it into place before anything else
	// reads the (seemingly empty) store.
	if _, err := os.Stat(tmp); err == nil {
		authPath := filepath.Join(root, authFileName)
		if _, err := os.Stat(authPath); os.IsNotExist(err) {
			if err := os.MkdirAll(root, 0700); err == nil {
				if err := os.Rename(tmp, authPath); err == nil {
					fmt.Fprintf(os.Stderr, "Recovered interrupted migration: %s -> %s\n", tmp, authPath)
				}
			}
		}
	}

	info, err := os.Stat(root)
	switch {
	case err == nil && info.IsDir():
		// Already the new layout.
	case err == nil:
		// Legacy token file occupies the directory's path: move it aside,
		// make the directory, and move it back in as auth.yml. A stranded
		// temp file from an unrecoverable earlier attempt is preserved as
		// .bak rather than overwritten.
		if _, err := os.Stat(tmp); err == nil {
			_ = os.Rename(tmp, tmp+".bak")
		}
		if err := os.Rename(root, tmp); err != nil {
			return root
		}
		if err := os.MkdirAll(root, 0700); err != nil {
			_ = os.Rename(tmp, root)
			return root
		}
		if err := os.Rename(tmp, filepath.Join(root, authFileName)); err != nil {
			_ = os.RemoveAll(root)
			_ = os.Rename(tmp, root)
			return root
		}
		fmt.Fprintf(os.Stderr, "Migrated %s to %s\n", root, filepath.Join(root, authFileName))
	default:
		if err := os.MkdirAll(root, 0700); err != nil {
			return root
		}
	}

	return root
}

// AuthFilePath returns the token-store file inside the resolved ~/.xurl
// directory (migrating any legacy layout first).
func AuthFilePath() string {
	dir := resolveStoreDir()
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		// Migration failed and the legacy file layout is still in effect.
		return dir
	}
	return filepath.Join(dir, authFileName)
}

// KeysFilePath returns the chat-key file inside the resolved ~/.xurl
// directory.
func KeysFilePath() string {
	return filepath.Join(resolveStoreDir(), keysFileName)
}
