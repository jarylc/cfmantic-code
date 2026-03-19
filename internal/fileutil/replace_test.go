package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplaceFileForOS_WindowsRetriesAfterRemovingExistingFile(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "state.json.tmp")
	filePath := filepath.Join(dir, "state.json")

	require.NoError(t, os.WriteFile(filePath, []byte("old"), 0o600))
	require.NoError(t, os.WriteFile(tmpPath, []byte("new"), 0o600))

	renameCalls := 0
	removeCalled := false

	err := replaceFileForOS(tmpPath, filePath, "windows",
		func(oldPath, newPath string) error {
			renameCalls++

			assert.Equal(t, tmpPath, oldPath)
			assert.Equal(t, filePath, newPath)

			if renameCalls == 1 {
				return os.ErrExist
			}

			return nil
		},
		os.Stat,
		func(name string) error {
			removeCalled = true

			assert.Equal(t, filePath, name)

			return nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, 2, renameCalls)
	assert.True(t, removeCalled)
}

func TestReplaceFileForOS_WindowsDoesNotRemoveDirectories(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "state.json.tmp")
	filePath := filepath.Join(dir, "state.json")

	require.NoError(t, os.WriteFile(tmpPath, []byte("new"), 0o600))
	require.NoError(t, os.Mkdir(filePath, 0o755))

	renameCalls := 0
	removeCalled := false

	err := replaceFileForOS(tmpPath, filePath, "windows",
		func(oldPath, newPath string) error {
			renameCalls++
			return os.ErrExist
		},
		os.Stat,
		func(name string) error {
			removeCalled = true
			return nil
		},
	)

	require.ErrorIs(t, err, os.ErrExist)
	assert.Equal(t, 1, renameCalls)
	assert.False(t, removeCalled)
}

func TestReplaceFile_RenamesTempFile(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "state.json.tmp")
	filePath := filepath.Join(dir, "state.json")

	require.NoError(t, os.WriteFile(tmpPath, []byte("new"), 0o600))

	require.NoError(t, ReplaceFile(tmpPath, filePath))

	contents, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), contents)

	_, err = os.Stat(tmpPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReplaceFileForOS_NonWindowsReturnsRenameError(t *testing.T) {
	renameErr := errors.New("rename failed")

	err := replaceFileForOS("tmp", "file", "linux",
		func(oldPath, newPath string) error {
			assert.Equal(t, "tmp", oldPath)
			assert.Equal(t, "file", newPath)

			return renameErr
		},
		func(name string) (os.FileInfo, error) {
			t.Fatalf("stat should not be called for non-windows rename failures")
			return nil, errors.New("unexpected stat call")
		},
		func(name string) error {
			t.Fatalf("remove should not be called for non-windows rename failures")
			return nil
		},
	)

	require.ErrorIs(t, err, renameErr)
}

func TestReplaceFileForOS_WindowsReturnsOriginalErrorWhenRemoveFails(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "state.json.tmp")
	filePath := filepath.Join(dir, "state.json")
	removeErr := errors.New("remove failed")

	require.NoError(t, os.WriteFile(tmpPath, []byte("new"), 0o600))
	require.NoError(t, os.WriteFile(filePath, []byte("old"), 0o600))

	err := replaceFileForOS(tmpPath, filePath, "windows",
		func(oldPath, newPath string) error {
			return os.ErrExist
		},
		os.Stat,
		func(name string) error {
			assert.Equal(t, filePath, name)
			return removeErr
		},
	)

	require.ErrorIs(t, err, os.ErrExist)
	assert.NotErrorIs(t, err, removeErr)
}
