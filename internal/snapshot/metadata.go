package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const MetadataDirName = ".cfmantic"

const metadataGitignoreContents = "*\n"

type metadataFile interface {
	WriteString(s string) (n int, err error)
	Close() error
}

var openMetadataFile = func(path string) (metadataFile, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

func MetadataDirPath(codebasePath string) string {
	return filepath.Join(codebasePath, MetadataDirName)
}

func EnsureMetadataDir(codebasePath string) error {
	return EnsureMetadataDirPath(MetadataDirPath(codebasePath))
}

func EnsureMetadataDirPath(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create metadata dir: %w", err)
	}

	return ensureMetadataGitignore(dir)
}

func ensureMetadataGitignore(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat metadata gitignore: %w", err)
	}

	f, err := openMetadataFile(path)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}

		return fmt.Errorf("create metadata gitignore: %w", err)
	}

	if _, err := f.WriteString(metadataGitignoreContents); err != nil {
		f.Close()       //nolint:gosec // G104: error path cleanup
		os.Remove(path) //nolint:gosec // G104: best-effort cleanup

		return fmt.Errorf("write metadata gitignore: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(path) //nolint:gosec // G104: best-effort cleanup

		return fmt.Errorf("close metadata gitignore: %w", err)
	}

	return nil
}
