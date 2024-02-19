package chunkeduploader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Craftserve/chunked-uploader/utils"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/spf13/afero"
	"golang.org/x/sync/singleflight"
)

// group is used to ensure that only one goroutine works on a task.
var group singleflight.Group

type StandardUmask = fs.FileMode

const (
	StandardAccess StandardUmask = 0755
	OnlyRead       StandardUmask = 0400
)

type ChunkedUploaderService struct {
	fs      afero.Fs
	rootDir string
}

func NewChunkedUploaderService(fs afero.Fs, rootDir string) *ChunkedUploaderService {
	return &ChunkedUploaderService{fs: fs, rootDir: rootDir}
}

func (c *ChunkedUploaderService) generateUploadId() string {
	return uuid.New().String()
}

// createUpload creates a new upload with a given uploadId and maxSize, it allocates the file with the given size.
func (c *ChunkedUploaderService) createUpload(uploadId string, maxSize int64) (err error) {
	tempPath := getUploadFilePath(c.rootDir, uploadId)
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
func (c *ChunkedUploaderService) writePart(path string, data io.Reader, offset int64, computeHash bool) (*string, error) {
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
func (c *ChunkedUploaderService) Cleanup(duration time.Duration) {
	uploadsPath := getUploadPath(c.rootDir)
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
func (c *ChunkedUploaderService) VerifyUpload(uploadId string, expectedChecksum string) error {
	pendingPath := getUploadFilePath(c.rootDir, uploadId)

	checksum, err := utils.ComputeChecksum(c.fs, pendingPath)
	if err != nil {
		return fmt.Errorf("Failed to compute checksum, " + err.Error())
	}

	if checksum != expectedChecksum {
		return fmt.Errorf("checksum does not match expected checksum")
	}

	return nil
}

type CreateUploadRequest struct {
	FileSize int64 `json:"file_size"`
}

// CreateUploadHandler creates a new upload with a given file size and returns an uploadId.
func (c *ChunkedUploaderService) CreateUploadHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateUploadRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if req.FileSize <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid file_size, must be a positive integer")
		return
	}

	uploadId := c.generateUploadId()

	err = c.createUpload(uploadId, req.FileSize)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to create upload: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"upload_id": uploadId})
}

// UploadChunkHandler uploads a chunk of a file to a given uploadId.
func (c *ChunkedUploaderService) UploadChunkHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uploadId := vars["upload_id"]

	if uploadId == "" {
		writeJSONError(w, http.StatusBadRequest, "upload_id is required")
		return
	}

	computeHash := r.URL.Query().Get("computeHash") == "true"

	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		writeJSONError(w, http.StatusBadRequest, "Range header is required")
		return
	}

	rangeStart, rangeEnd, err := parseRangeHeader(rangeHeader)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid Range header")
		return
	}

	if rangeStart > rangeEnd {
		writeJSONError(w, http.StatusBadRequest, "Invalid Range header")
		return
	}

	tempPath := getUploadFilePath(c.rootDir, uploadId)

	err = r.ParseMultipartForm(100 << 20) // 100 MB max memory
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to parse multipart form: "+err.Error())
		return
	}

	requestFile, _, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "file is required")
		return
	}

	defer requestFile.Close()

	h, err := c.writePart(tempPath, requestFile, rangeStart, computeHash)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to write part: "+err.Error())
		return
	}

	if h != nil {
		w.Header().Set("X-Checksum", *h)
	}

	w.WriteHeader(http.StatusCreated)
}

type FinishUploadRequest struct {
	Checksum string `json:"checksum"`
}

// FinishUploadHandler finishes an upload by verifying the checksum of the uploaded file.
func (c *ChunkedUploaderService) FinishUploadHandler(w http.ResponseWriter, r *http.Request) {
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

	err = c.VerifyUpload(uploadId, expectedChecksum)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to verify upload: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

// OpenUploadedFileHandler opens an uploaded file with a given uploadId and returns a file handle.
func (c *ChunkedUploaderService) OpenUploadedFileHandler(upload_id string) (io.ReadCloser, error) {
	path := getUploadFilePath(c.rootDir, upload_id)
	file, err := c.fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file: %w", err)
	}
	return file, nil
}

type RenameUploadedFileRequest struct {
	Path string
}

// RenameUploadedFileHandler renames an uploaded file to a given path.
func (c *ChunkedUploaderService) RenameUploadedFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uploadId := vars["upload_id"]

	if uploadId == "" {
		writeJSONError(w, http.StatusBadRequest, "upload_id is required")
		return
	}

	var req RenameUploadedFileRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	uploadPath := getUploadFilePath(c.rootDir, uploadId)
	dir := filepath.Dir(req.Path)
	if err := c.fs.MkdirAll(dir, StandardAccess); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create directory: "+err.Error())
		return
	}

	err = c.fs.Rename(uploadPath, req.Path)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to rename file: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
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

// parseRangeHeader parses a range header and returns the start and end of the range.
func parseRangeHeader(rangeHeader string) (int64, int64, error) {
	var start, end int64
	_, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

// writeJSONError writes a JSON error response with a given status code and message.
func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func getUploadFilePath(rootDir string, uploadId string) string {
	return fmt.Sprintf("%s/.pending/%s", rootDir, uploadId)
}

func getUploadPath(rootDir string) string {
	return fmt.Sprintf("%s/.pending", rootDir)
}
