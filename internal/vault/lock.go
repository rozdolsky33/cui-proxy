package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrInUse means another writer holds the file vault's lifetime lock.
var ErrInUse = errors.New("vault: file is already in use")

// LockFile exclusively locks path + ".lock" for a file vault. The caller must
// acquire it before loading or creating the vault and close it only after the
// final save. The lock file stays in place: unlinking it would allow a second
// writer to lock a different inode while the first writer still holds its lock.
func LockFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("vault: lock directory: %w", err)
	}
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("vault: lock: %w", err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("vault: lock: %w", err)
	}
	st, owned := fi.Sys().(*syscall.Stat_t)
	if !fi.Mode().IsRegular() || !owned || int(st.Uid) != os.Getuid() || fi.Mode().Perm()&^0o600 != 0 {
		f.Close()
		return nil, errors.New("vault: lock must be a regular file owned by this user with mode 0600 or stricter")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("%w: %s", ErrInUse, path)
		}
		return nil, fmt.Errorf("vault: lock: %w", err)
	}
	return f, nil
}
