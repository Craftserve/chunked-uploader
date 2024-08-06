package chunkeduploader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Craftserve/chunked-uploader/utils"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/spf13/afero"
)

type StandardUmask = fs.FileMode

const (
	StandardAccess StandardUmask = 0755
	OnlyRead       StandardUmask = 0400
)

type ChunkedUploaderService struct {
	fs          afero.Fs
	maxFileSize *int64
}

func NewChunkedUploaderService(fs afero.Fs, maxFileSize *int64) *ChunkedUploaderService {
	return &ChunkedUploaderService{fs: fs, maxFileSize: maxFileSize}
}

func (c *ChunkedUploaderService) generateUploadId() string {
	return uuid.New().String()
}

// createUpload creates a new upload with a given uploadId and maxSize, it allocates the file with the given size.
func (c *ChunkedUploaderService) createUpload(uploadId string, maxSize int64) (err error) {
	tempPath := getUploadFilePath(uploadId)
	file, err := createFile(c.fs, tempPath)

	if err != nil {
		return fmt.Errorf("Failed to create temp file: "+err.Error(), http.StatusInternalServerError)
	}

	defer file.Close()

	if maxSize > 0 {
		err = file.Truncate(maxSize)
		if err != nil {
			return fmt.Errorf("Failed to preallocate file size: "+err.Error(), http.StatusInternalServerError)
		}
	}

	return err
}

// writePart writes a part of a file to a given path.
func (c *ChunkedUploaderService) writePart(path string, reader io.Reader, offset int64) (h string, err error) {
	var writer io.Writer
	var hasher hash.Hash = sha256.New()

	file, err := c.fs.OpenFile(path, os.O_WRONLY, 0644)
	if err != nil {
		return h, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return h, err
	}

	if c.maxFileSize != nil {
		if fileInfo.Size() >= *c.maxFileSize {
			return h, fmt.Errorf("file_size_exceeds_maximum")
		}
	}

	writer = io.MultiWriter(file, hasher)

	if offset != -1 {
		_, err = file.Seek(offset, io.SeekStart)
		if err != nil {
			return h, err
		}
	}

	if offset == -1 {
		_, err = file.Seek(0, io.SeekEnd)
		if err != nil {
			return h, err
		}
	}

	_, err = io.Copy(writer, reader)
	if err != nil {
		return h, err
	}

	h = hex.EncodeToString(hasher.Sum(nil))

	return h, nil
}

// Cleanup removes old uploads that were created before a given timeLimit.
func (c *ChunkedUploaderService) Cleanup(duration time.Duration) error {
	timeLimit := time.Now().Add(-duration)

	afero.Walk(c.fs, ".pending", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.ModTime().Before(timeLimit) {
			log.Printf("[ChunkedUploaderService] Removing old upload: %s, modified at: %s, now is: %s", path, info.ModTime(), time.Now())
			return c.fs.Remove(path)
		}

		return nil
	})

	return nil
}

// Remove pending temporary file
func (c *ChunkedUploaderService) RemovePendingFile(uploadId string) error {
	path := getUploadFilePath(uploadId)
	err := c.fs.Remove(path)
	if err != nil {
		return fmt.Errorf("Failed to remove pending file: " + err.Error())
	}

	return nil
}

// VerifyUpload verifies an upload by comparing the checksum of the uploaded file with an expected checksum.
func (c *ChunkedUploaderService) verifyUpload(uploadId string, expectedChecksum string) error {
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

func (c *ChunkedUploaderService) CreateUpload(fileSize int64) (string, error) {
	uploadId := c.generateUploadId()
	err := c.createUpload(uploadId, fileSize)
	if err != nil {
		return "", fmt.Errorf("Failed to create upload: " + err.Error())
	}
	return uploadId, nil
}

func (c *ChunkedUploaderService) UploadChunk(uploadId string, data io.Reader, offset int64) (string, error) {
	tempPath := getUploadFilePath(uploadId)
	return c.writePart(tempPath, data, offset)
}

func (c *ChunkedUploaderService) FinishUpload(uploadId string, expectedChecksum string) (path string, err error) {
	err = c.verifyUpload(uploadId, expectedChecksum)
	if err != nil {
		return "", fmt.Errorf("Failed to verify upload: " + err.Error())
	}

	path = getUploadFilePath(uploadId)

	return path, nil
}

func (c *ChunkedUploaderService) OpenUploadedFile(uploadId string) (io.ReadCloser, error) {
	path := getUploadFilePath(uploadId)
	file, err := c.fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file: %w", err)
	}

	return file, nil
}

type ChunkedUploaderHandler struct {
	service *ChunkedUploaderService
}

func NewChunkedUploaderHandler(service *ChunkedUploaderService) *ChunkedUploaderHandler {
	return &ChunkedUploaderHandler{service: service}
}

type CreateUploadRequest struct {
	FileSize *int64 `json:"file_size"`
}

// CreateUploadHandler creates a new upload with a given file size and returns an uploadId.
func (c *ChunkedUploaderHandler) CreateUploadHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateUploadRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	var fileSize int64 = -1
	if req.FileSize != nil {
		fileSize = *req.FileSize
	}

	uploadId, err := c.service.CreateUpload(fileSize)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to create upload: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"upload_id": uploadId})
}

// UploadChunkHandler uploads a chunk of a file to a given uploadId.
func (c *ChunkedUploaderHandler) UploadChunkHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uploadId := vars["upload_id"]

	if uploadId == "" {
		writeJSONError(w, http.StatusBadRequest, "upload_id is required")
		return
	}

	rangeHeader := r.Header.Get("Range")

	var rangeStart int64 = -1

	if rangeHeader != "" {
		var err error
		rangeStart, err = parseRangeHeader(rangeHeader)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "Invalid Range header")
			return
		}
	}

	// it will be io.Reader sent wit application/octet-stream
	fileReader := r.Body

	h, err := c.service.UploadChunk(uploadId, fileReader, rangeStart)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to upload chunk: "+err.Error())
		return
	}

	w.Header().Set("X-Checksum", h)
	w.WriteHeader(http.StatusOK)
}

type FinishUploadRequest struct {
	Checksum string `json:"checksum"`
}

// FinishUploadHandler finishes an upload by verifying the checksum of the uploaded file.
func (c *ChunkedUploaderHandler) FinishUploadHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uploadId := vars["upload_id"]

	if uploadId == "" {
		writeJSONError(w, http.StatusBadRequest, "upload_id is required")
		return
	}

	var req FinishUploadRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	expectedChecksum := req.Checksum

	if expectedChecksum == "" {
		writeJSONError(w, http.StatusBadRequest, "checksum is required")
		return
	}

	path, err := c.service.FinishUpload(uploadId, expectedChecksum)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to verify upload: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"path": path})
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
	return openFile(fs, path, os.O_RDWR|os.O_CREATE, StandardAccess)
}

// parseRangeHeader parses a range header and returns range start
func parseRangeHeader(rangeHeader string) (int64, error) {
	var start int64
	_, err := fmt.Sscanf(rangeHeader, "offset=%d-", &start)
	if err != nil {
		return 0, err
	}
	return start, nil
}

// writeJSONError writes a JSON error response with a given status code and message.
func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func getUploadFilePath(uploadId string) string {
	return filepath.Join("/.pending", uploadId)
}
