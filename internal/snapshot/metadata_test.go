package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubMetadataFile struct {
	writeErr error
	closeErr error
}

func (f *stubMetadataFile) WriteString(s string) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}

	return len(s), nil
}

func (f *stubMetadataFile) Close() error {
	return f.closeErr
}

func TestMetadataDirPath(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, filepath.Join(dir, MetadataDirName), MetadataDirPath(dir))
}

func TestEnsureMetadataDir_CreatesMetadataDirAndGitignore(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, EnsureMetadataDir(dir))

	info, err := os.Stat(MetadataDirPath(dir))
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	gitignore, err := os.ReadFile(filepath.Join(MetadataDirPath(dir), ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, metadataGitignoreContents, string(gitignore))
}

func TestEnsureMetadataDirPath_DoesNotOverwriteExistingGitignore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), MetadataDirName)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("keep-me\n"), 0o600))

	require.NoError(t, EnsureMetadataDirPath(dir))

	gitignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "keep-me\n", string(gitignore))
}

func TestEnsureMetadataGitignore_Errors(t *testing.T) {
	t.Run("stat error returns wrapped error", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "not-a-dir")
		require.NoError(t, os.WriteFile(dir, []byte("x"), 0o600))

		err := ensureMetadataGitignore(dir)
		require.Error(t, err)
		assert.ErrorContains(t, err, "stat metadata gitignore")
	})

	t.Run("open race with existing file is ignored", func(t *testing.T) {
		oldOpen := openMetadataFile
		openMetadataFile = func(string) (metadataFile, error) {
			return nil, os.ErrExist
		}

		t.Cleanup(func() { openMetadataFile = oldOpen })

		require.NoError(t, ensureMetadataGitignore(t.TempDir()))
	})

	t.Run("open error returns wrapped error", func(t *testing.T) {
		err := ensureMetadataGitignore(filepath.Join(t.TempDir(), "missing", MetadataDirName))
		require.Error(t, err)
		assert.ErrorContains(t, err, "create metadata gitignore")
	})

	t.Run("write error returns wrapped error", func(t *testing.T) {
		oldOpen := openMetadataFile
		openMetadataFile = func(string) (metadataFile, error) {
			return &stubMetadataFile{writeErr: errors.New("write failed")}, nil
		}

		t.Cleanup(func() { openMetadataFile = oldOpen })

		err := ensureMetadataGitignore(t.TempDir())
		require.Error(t, err)
		assert.ErrorContains(t, err, "write metadata gitignore")
	})

	t.Run("close error returns wrapped error", func(t *testing.T) {
		oldOpen := openMetadataFile
		openMetadataFile = func(string) (metadataFile, error) {
			return &stubMetadataFile{closeErr: errors.New("close failed")}, nil
		}

		t.Cleanup(func() { openMetadataFile = oldOpen })

		err := ensureMetadataGitignore(t.TempDir())
		require.Error(t, err)
		assert.ErrorContains(t, err, "close metadata gitignore")
	})
}
