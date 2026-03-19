package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTempLockFile struct {
	name     string
	writeErr error
	closeErr error
}

func (f *stubTempLockFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}

	return len(p), nil
}

func (f *stubTempLockFile) Close() error {
	return f.closeErr
}

func (f *stubTempLockFile) Name() string {
	return f.name
}

func writeLockInfoFile(t *testing.T, dir string, info lockInfo) {
	t.Helper()

	fp := LockFilePath(dir)
	require.NoError(t, os.MkdirAll(filepath.Dir(fp), 0o755))

	data, err := json.Marshal(info)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(fp, data, 0o644))
}

func TestLockFilePath(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, filepath.Join(dir, ".cfmantic", ".lock"), LockFilePath(dir))
}

func TestHasActiveLock(t *testing.T) {
	t.Run("missing lock", func(t *testing.T) {
		assert.False(t, HasActiveLock(t.TempDir()))
	})

	t.Run("live process with stale timestamp", func(t *testing.T) {
		dir := t.TempDir()
		writeLockInfoFile(t, dir, lockInfo{PID: os.Getpid(), StartedAt: time.Now().Add(-(lockStaleAfter + time.Minute))})

		assert.True(t, HasActiveLock(dir))
	})

	t.Run("dead process", func(t *testing.T) {
		dir := t.TempDir()
		writeLockInfoFile(t, dir, lockInfo{PID: 99999999, StartedAt: time.Now()})

		assert.False(t, HasActiveLock(dir))
	})
}

func TestAcquireLock_Fresh(t *testing.T) {
	dir := t.TempDir()

	release, err := AcquireLock(dir)
	require.NoError(t, err)
	require.NotNil(t, release)

	_, statErr := os.Stat(LockFilePath(dir))
	require.NoError(t, statErr, "lock file should exist after acquire")

	gitignore, err := os.ReadFile(filepath.Join(MetadataDirPath(dir), ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "*", strings.TrimSpace(string(gitignore)))

	release()

	_, statErr = os.Stat(LockFilePath(dir))
	assert.True(t, os.IsNotExist(statErr), "lock file should be deleted after release")
}

func TestAcquireLock_LiveProcessIgnoresAge(t *testing.T) {
	dir := t.TempDir()
	writeLockInfoFile(t, dir, lockInfo{PID: os.Getpid(), StartedAt: time.Now().Add(-(lockStaleAfter + time.Minute))})

	release, err := AcquireLock(dir)
	require.Error(t, err)
	assert.Nil(t, release)
	assert.Contains(t, err.Error(), fmt.Sprintf("locked by PID %d", os.Getpid()))
}

func TestAcquireLock_DeadProcess(t *testing.T) {
	dir := t.TempDir()
	writeLockInfoFile(t, dir, lockInfo{PID: 99999999, StartedAt: time.Now()})

	release, err := AcquireLock(dir)
	require.NoError(t, err)
	require.NotNil(t, release)

	release()
}

func TestAcquireLock_DeadProcessWithStaleTimestamp(t *testing.T) {
	dir := t.TempDir()
	writeLockInfoFile(t, dir, lockInfo{PID: 99999999, StartedAt: time.Now().Add(-(lockStaleAfter + time.Minute))})

	release, err := AcquireLock(dir)
	require.NoError(t, err)
	require.NotNil(t, release)

	release()
}

func TestAcquireLock_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	indexPath := MetadataDirPath(dir)
	require.NoError(t, os.WriteFile(indexPath, []byte("conflict"), 0o644))

	release, err := AcquireLock(dir)
	require.Error(t, err)
	assert.Nil(t, release)
	assert.Contains(t, err.Error(), "create lock dir")
}

func TestAcquireLock_WriteFileError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod restriction bypassed as root")
	}

	dir := t.TempDir()
	indexDir := MetadataDirPath(dir)
	require.NoError(t, os.MkdirAll(indexDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(indexDir, ".gitignore"), []byte("*\n"), 0o644))
	require.NoError(t, os.Chmod(indexDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(indexDir, 0o755) })

	release, err := AcquireLock(dir)
	require.Error(t, err)
	assert.Nil(t, release)
	assert.Contains(t, err.Error(), "write lock")
}

func TestAcquireLock_InvalidJSONLock(t *testing.T) {
	dir := t.TempDir()
	fp := LockFilePath(dir)
	require.NoError(t, os.MkdirAll(filepath.Dir(fp), 0o755))
	require.NoError(t, os.WriteFile(fp, []byte("not valid json {{"), 0o644))

	release, err := AcquireLock(dir)
	require.NoError(t, err)
	require.NotNil(t, release)

	release()
}

func TestAcquireLock_ConcurrentAtomicity(t *testing.T) {
	dir := t.TempDir()

	const goroutines = 50

	var (
		mu       sync.Mutex
		releases []func()
		failures int
	)

	barrier := make(chan struct{})

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			<-barrier

			release, err := AcquireLock(dir)

			mu.Lock()
			defer mu.Unlock()

			if err == nil {
				releases = append(releases, release)
			} else {
				failures++
			}
		})
	}

	close(barrier)
	wg.Wait()

	assert.Len(t, releases, 1, "exactly one goroutine should acquire the lock")
	assert.Equal(t, goroutines-1, failures, "all others should fail")

	for _, release := range releases {
		release()
	}
}

func TestAcquireLock_RemoveStaleError(t *testing.T) {
	dir := t.TempDir()
	fp := LockFilePath(dir)
	indexDir := filepath.Dir(fp)
	require.NoError(t, os.MkdirAll(indexDir, 0o755))

	require.NoError(t, os.MkdirAll(fp, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fp, "sentinel"), []byte("x"), 0o644))

	release, err := AcquireLock(dir)
	require.Error(t, err)
	assert.Nil(t, release)
	assert.ErrorContains(t, err, "remove stale")
}

func TestProcessAlive(t *testing.T) {
	t.Run("current process is alive", func(t *testing.T) {
		assert.True(t, processAlive(os.Getpid()))
	})

	t.Run("non-existent PID is not alive", func(t *testing.T) {
		assert.False(t, processAlive(99999999))
	})

	t.Run("find process error returns false", func(t *testing.T) {
		oldFindProcess := findProcessByPID
		findProcessByPID = func(int) (*os.Process, error) {
			return nil, errors.New("boom")
		}

		t.Cleanup(func() { findProcessByPID = oldFindProcess })

		assert.False(t, processAlive(1))
	})
}

func TestIsActiveLock_ZeroStartTime(t *testing.T) {
	assert.False(t, isActiveLock(lockInfo{PID: os.Getpid()}))
}

func TestAtomicCreateLock_Errors(t *testing.T) {
	t.Run("marshal error returns wrapped error", func(t *testing.T) {
		oldMarshal := marshalLockInfo
		marshalLockInfo = func(any) ([]byte, error) {
			return nil, errors.New("marshal failed")
		}

		t.Cleanup(func() { marshalLockInfo = oldMarshal })

		_, err := atomicCreateLock(filepath.Join(t.TempDir(), ".lock"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "marshal lock info")
	})

	t.Run("write error returns wrapped error", func(t *testing.T) {
		oldCreateTemp := createTempLockFile
		createTempLockFile = func(string, string) (tempLockFile, error) {
			return &stubTempLockFile{name: filepath.Join(t.TempDir(), ".lock.tmp"), writeErr: errors.New("write failed")}, nil
		}

		t.Cleanup(func() { createTempLockFile = oldCreateTemp })

		_, err := atomicCreateLock(filepath.Join(t.TempDir(), ".lock"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "write lock")
	})

	t.Run("close error returns wrapped error", func(t *testing.T) {
		oldCreateTemp := createTempLockFile
		createTempLockFile = func(string, string) (tempLockFile, error) {
			return &stubTempLockFile{name: filepath.Join(t.TempDir(), ".lock.tmp"), closeErr: errors.New("close failed")}, nil
		}

		t.Cleanup(func() { createTempLockFile = oldCreateTemp })

		_, err := atomicCreateLock(filepath.Join(t.TempDir(), ".lock"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "write lock")
	})

	t.Run("link error returns wrapped error", func(t *testing.T) {
		oldCreateTemp := createTempLockFile
		createTempLockFile = func(string, string) (tempLockFile, error) {
			return &stubTempLockFile{name: filepath.Join(t.TempDir(), "missing.tmp")}, nil
		}

		t.Cleanup(func() { createTempLockFile = oldCreateTemp })

		_, err := atomicCreateLock(filepath.Join(t.TempDir(), ".lock"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "write lock")
	})
}
