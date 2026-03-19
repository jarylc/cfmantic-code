package fileutil

import (
	"errors"
	"os"
	"runtime"
)

type (
	renameFileFunc func(oldPath, newPath string) error
	statFileFunc   func(name string) (os.FileInfo, error)
	removeFileFunc func(name string) error
)

// ReplaceFile promotes tmpPath into filePath.
// On Windows, os.Rename cannot overwrite an existing file, so an os.ErrExist
// from renaming a regular file falls back to remove-then-rename.
func ReplaceFile(tmpPath, filePath string) error {
	return replaceFileForOS(tmpPath, filePath, runtime.GOOS, os.Rename, os.Stat, os.Remove)
}

func replaceFileForOS(
	tmpPath, filePath, goos string,
	rename renameFileFunc,
	stat statFileFunc,
	remove removeFileFunc,
) error {
	err := rename(tmpPath, filePath)
	if err == nil {
		return nil
	}

	if goos != "windows" || !errors.Is(err, os.ErrExist) {
		return err
	}

	info, statErr := stat(filePath)
	if statErr != nil || info.IsDir() {
		return err
	}

	if removeErr := remove(filePath); removeErr != nil {
		return err
	}

	return rename(tmpPath, filePath)
}
