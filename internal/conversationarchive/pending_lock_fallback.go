//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package conversationarchive

import "os"

// The production targets use advisory locks. This exclusive-create fallback
// keeps less common targets buildable, but interrupted locks require manual
// removal before recovery.
func acquirePendingLock(path string) (*os.File, bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if os.IsExist(err) {
		return nil, false, nil
	}
	return file, err == nil, err
}

func releasePendingLock(file *os.File) error {
	if file == nil {
		return nil
	}
	name := file.Name()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}
