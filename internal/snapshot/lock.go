package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrLocked is returned when the codebase lock is held by a live process.
var ErrLocked = errors.New("lock is held")

type tempLockFile interface {
	Write(p []byte) (n int, err error)
	Close() error
	Name() string
}

var (
	marshalLockInfo    = json.Marshal
	createTempLockFile = func(dir, pattern string) (tempLockFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	findProcessByPID = os.FindProcess
)

type lockInfo struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

const lockStaleAfter = 30 * time.Minute

// LockFilePath returns the lockfile path for a codebase: <path>/.cfmantic/.lock
func LockFilePath(codebasePath string) string {
	return filepath.Join(MetadataDirPath(codebasePath), ".lock")
}

// HasActiveLock reports whether the codebase lock is currently held by a live
// process.
func HasActiveLock(codebasePath string) bool {
	info, err := readLockInfo(LockFilePath(codebasePath))
	if err != nil {
		return false
	}

	return isActiveLock(info)
}

// AcquireLock creates an exclusive lockfile for the codebase.  The lock is
// obtained atomically: lock content is written to a temp file first, then
// promoted to the canonical path via os.Link which fails with os.ErrExist when
// the target already exists.  This ensures the canonical lock file always
// contains complete JSON when it first becomes visible to other processes.
//
// Returns a release function that deletes the lock, or an error if the codebase
// is already locked by a live process.
func AcquireLock(codebasePath string) (func(), error) {
	fp := LockFilePath(codebasePath)

	if err := EnsureMetadataDir(codebasePath); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}

	// Attempt 1: write to temp then atomically link to the canonical path.
	release, err := atomicCreateLock(fp)
	if err == nil {
		return release, nil
	}

	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}

	// Canonical path already exists — decide whether it belongs to a live process.
	if existing, readErr := readLockInfo(fp); readErr == nil {
		if isActiveLock(existing) {
			return nil, fmt.Errorf("%w: locked by PID %d since %s",
				ErrLocked, existing.PID, existing.StartedAt.Format(time.RFC3339))
		}

		log.Printf("lock: removing stale lock (PID %d, %s old)",
			existing.PID, time.Since(existing.StartedAt).Round(time.Second))
	}

	// Stale or unreadable lock ��� remove it and retry once.
	if removeErr := os.Remove(fp); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return nil, fmt.Errorf("write lock: remove stale: %w", removeErr)
	}

	// Attempt 2: single retry after removing the stale lock.
	return atomicCreateLock(fp)
}

// atomicCreateLock writes the lock payload to a temporary file in the same
// directory as fp, then calls os.Link(tmp, fp).  os.Link is a POSIX atomic
// operation that fails with os.ErrExist when fp already exists, so the
// canonical path is always either absent or contains complete, valid JSON.
//
// Returns os.ErrExist (unwrapped) when fp is already present so the caller can
// distinguish that case from other I/O failures.
func atomicCreateLock(fp string) (func(), error) {
	info := lockInfo{PID: os.Getpid(), StartedAt: time.Now()}

	data, err := marshalLockInfo(info)
	if err != nil {
		return nil, fmt.Errorf("marshal lock info: %w", err)
	}

	// Write content to a unique temp file in the same directory so that the
	// hard-link below is always a same-filesystem operation.
	tmp, err := createTempLockFile(filepath.Dir(fp), ".lock.tmp.*")
	if err != nil {
		return nil, fmt.Errorf("write lock: %w", err)
	}

	tmpPath := tmp.Name()

	if _, writeErr := tmp.Write(data); writeErr != nil {
		tmp.Close()        //nolint:gosec // G104: error path — write error takes priority
		os.Remove(tmpPath) //nolint:gosec // G104: error path cleanup

		return nil, fmt.Errorf("write lock: %w", writeErr)
	}

	if closeErr := tmp.Close(); closeErr != nil {
		os.Remove(tmpPath) //nolint:gosec // G104: error path cleanup

		return nil, fmt.Errorf("write lock: %w", closeErr)
	}

	// Atomically promote: os.Link fails with ErrExist if fp already exists.
	linkErr := os.Link(tmpPath, fp)
	os.Remove(tmpPath) //nolint:gosec // G104: temp file cleanup, error irrelevant

	if linkErr != nil {
		if errors.Is(linkErr, os.ErrExist) {
			return nil, os.ErrExist
		}

		return nil, fmt.Errorf("write lock: %w", linkErr)
	}

	return func() {
		os.Remove(fp) //nolint:gosec // G104: best-effort cleanup
	}, nil
}

func readLockInfo(fp string) (lockInfo, error) {
	data, err := os.ReadFile(fp)
	if err != nil {
		return lockInfo{}, fmt.Errorf("read lock file: %w", err)
	}

	var info lockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return lockInfo{}, fmt.Errorf("unmarshal lock info: %w", err)
	}

	return info, nil
}

func isActiveLock(info lockInfo) bool {
	if info.StartedAt.IsZero() {
		return false
	}

	return processAlive(info.PID)
}

// processAlive checks whether a process with the given PID is still running.
func processAlive(pid int) bool {
	p, err := findProcessByPID(pid)
	if err != nil {
		return false
	}

	return p.Signal(syscall.Signal(0)) == nil
}
