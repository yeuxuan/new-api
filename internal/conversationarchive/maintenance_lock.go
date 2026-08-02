package conversationarchive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MaintenanceLock serializes archive migration, recovery, verification,
// reindexing, and export commands that share one archive root.
type MaintenanceLock struct {
	file *os.File
}

func (s *Store) AcquireMaintenanceLock() (*MaintenanceLock, error) {
	if s == nil {
		return nil, errors.New("conversation archive store is nil")
	}
	if err := s.checkMountSentinel(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.root, ".conversation-archive-maintenance.lock")
	file, locked, err := acquirePendingLock(path)
	if err != nil {
		return nil, fmt.Errorf("acquire conversation archive maintenance lock: %w", err)
	}
	if !locked {
		return nil, errors.New("another conversation archive maintenance command is already running")
	}
	return &MaintenanceLock{file: file}, nil
}

func (l *MaintenanceLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := releasePendingLock(l.file)
	l.file = nil
	return err
}
