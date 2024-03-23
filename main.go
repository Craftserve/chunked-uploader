package chunkeduploader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Craftserve/chunked-uploader/pkg/utils"

	"github.com/google/uuid"
	"github.com/spf13/afero"
)

type StandardUmask = fs.FileMode

const (
	StandardAccess StandardUmask = 0755
	OnlyRead       StandardUmask = 0400
)

type Service struct {
	fs afero.Fs
}

func NewService(fs afero.Fs) *Service {
	return &Service{fs: fs}
}

func (c *Service) generateUploadId() string {
	return uuid.New().String()
}

// createUpload creates a new upload with a given uploadId and maxSize, it allocates the file with the given size.
func (c *Service) createUpload(uploadId string, maxSize int64) (err error) {
	tempPath := getUploadFilePath(uploadId)
	file, err := createFile(c.fs, tempPath)

	if err != nil {
		return fmt.Errorf("Failed to create temp file: "+err.Error(), http.StatusInternalServerError)
	}

	defer file.Close()

	err = file.Truncate(maxSize)
	if err != nil {
		return fmt.Errorf("Failed to preallocate file size: "+err.Error(), http.StatusInternalServerError)
	}

	return err
}

// writePart writes a part of a file to a given path.
func (c *Service) writePart(path string, data io.Reader, offset int64, computeHash bool) (*string, error) {
	var writer io.Writer
	var hasher hash.Hash

	file, err := c.fs.OpenFile(path, os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if computeHash {
		hasher = sha256.New()
		writer = io.MultiWriter(file, hasher)
	} else {
		writer = file
	}

	_, err = file.Seek(offset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(writer, data)
	if err != nil {
		return nil, err
	}

	if computeHash {
		hash := hex.EncodeToString(hasher.Sum(nil))
		return &hash, nil
	}

	return nil, nil
}

// Cleanup removes old uploads that were created before a given timeLimit.
func (c *Service) Cleanup(duration time.Duration) {
	uploadsPath := getUploadPath()
	timeLimit := time.Now().Add(-duration)

	afero.Walk(c.fs, uploadsPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.ModTime().Before(timeLimit) {
			return c.fs.Remove(path)
		}

		return nil
	})
}

// VerifyUpload verifies an upload by comparing the checksum of the uploaded file with an expected checksum.
func (c *Service) verifyUpload(uploadId string, expectedChecksum string) error {
	pendingPath := getUploadFilePath(uploadId)

	checksum, err := utils.ComputeChecksum(c.fs, pendingPath)
	if err != nil {
		return fmt.Errorf("Failed to compute checksum, "+err.Error()+"Pending path: ", pendingPath)
	}

	if checksum != expectedChecksum {
		return fmt.Errorf("checksum does not match expected checksum")
	}

	return nil
}

func (c *Service) CreateUpload(fileSize int64) (string, error) {
	uploadId := c.generateUploadId()
	err := c.createUpload(uploadId, fileSize)
	if err != nil {
		return "", fmt.Errorf("Failed to create upload: " + err.Error())
	}
	return uploadId, nil
}

func (c *Service) UploadChunk(uploadId string, data io.Reader, offset int64, computeHash bool) (*string, error) {
	tempPath := getUploadFilePath(uploadId)
	return c.writePart(tempPath, data, offset, computeHash)
}

func (c *Service) FinishUpload(uploadId string, expectedChecksum string) (path string, err error) {
	err = c.verifyUpload(uploadId, expectedChecksum)
	if err != nil {
		return "", fmt.Errorf("Failed to verify upload: " + err.Error())
	}

	path = getUploadFilePath(uploadId)

	return path, nil
}

func (c *Service) OpenUploadedFile(uploadId string) (io.ReadCloser, error) {
	path := getUploadFilePath(uploadId)
	file, err := c.fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file: %w", err)
	}

	return file, nil
}

func (c *Service) RenameUploadedFile(uploadId string, newPath string) error {
	uploadPath := getUploadFilePath(uploadId)

	dir := filepath.Dir(newPath)
	if err := c.fs.MkdirAll(dir, StandardAccess); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return c.fs.Rename(uploadPath, newPath)
}

// openFile opens a file with a given path and returns a file handle, it creates the directory if it does not exist.
func openFile(fs afero.Fs, path string, flag int, perm os.FileMode) (file afero.File, err error) {
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

// createFile creates a file with a given path and returns a file handle.
func createFile(fs afero.Fs, path string) (file afero.File, err error) {
	return openFile(fs, path, os.O_CREATE|os.O_RDWR, StandardAccess)
}

func getUploadFilePath(uploadId string) string {
	return filepath.Join(".pending", uploadId)
}

func getUploadPath() string {
	return ".pending"
}
