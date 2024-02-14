package files

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/afero"
)

type StandardUmask = fs.FileMode

const (
	StandardAccess StandardUmask = 0755
	OnlyRead       StandardUmask = 0400
)

func GetPendingFilePath(uploadId string, filename string) string {
	return fmt.Sprintf("./.chunked_uploader/.pending/%s/%s", uploadId, filename)
}

func GetPendingPath(uploadId string) string {
	return fmt.Sprintf("./.chunked_uploader/.pending/%s", uploadId)
}

func GetMetadataFilePath(uploadId string) string {
	return fmt.Sprintf("./.chunked_uploader/.pending/%s/metadata.json", uploadId)
}

func LockFile(file afero.File) error {
	realFile, ok := file.(*os.File)
	if !ok {
		return fmt.Errorf("not a real file")
	}

	err := syscall.Flock(int(realFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		return err
	}
	return nil
}

func UnlockFile(file afero.File) error {
	realFile, ok := file.(*os.File)
	if !ok {
		return fmt.Errorf("not a real file")
	}

	return syscall.Flock(int(realFile.Fd()), syscall.LOCK_UN)
}

func OpenFile(fs afero.Fs, path string, flag int, perm os.FileMode) (file afero.File, err error) {
	dir := filepath.Dir(path)
	if err := fs.MkdirAll(dir, perm); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	file, err = fs.OpenFile(path, flag, perm)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

func CreateFile(fs afero.Fs, path string) (file afero.File, err error) {
	return OpenFile(fs, path, os.O_CREATE|os.O_RDWR, StandardAccess)
}
