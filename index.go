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
)

type StandardUmask = fs.FileMode

const (
	StandardAccess StandardUmask = 0755
	OnlyRead       StandardUmask = 0400
)

type ChunkedUploaderService struct {
	fs afero.Fs
}

func NewChunkedUploaderService(fs afero.Fs) *ChunkedUploaderService {
	return &ChunkedUploaderService{fs: fs}
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
func (c *ChunkedUploaderService) verifyUpload(uploadId string, expectedChecksum string) error {
	pendingPath := getUploadFilePath(uploadId)

	checksum, err := utils.ComputeChecksum(c.fs, pendingPath)
	if err != nil {
		return fmt.Errorf("Failed to compute checksum, " + err.Error())
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

func (c *ChunkedUploaderService) UploadChunk(uploadId string, data io.Reader, offset int64, computeHash bool) (*string, error) {
	tempPath := getUploadFilePath(uploadId)
	return c.writePart(tempPath, data, offset, computeHash)
}

func (c *ChunkedUploaderService) FinishUpload(uploadId string, expectedChecksum string) error {
	return c.verifyUpload(uploadId, expectedChecksum)
}

func (c *ChunkedUploaderService) OpenUploadedFile(uploadId string) (io.ReadCloser, error) {
	path := getUploadFilePath(uploadId)
	file, err := c.fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file: %w", err)
	}

	return file, nil
}

func (c *ChunkedUploaderService) RenameUploadedFile(uploadId string, newPath string) error {
	uploadPath := getUploadFilePath(uploadId)

	dir := filepath.Dir(newPath)
	if err := c.fs.MkdirAll(dir, StandardAccess); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return c.fs.Rename(uploadPath, newPath)
}

type ChunkedUploaderHandler struct {
	service *ChunkedUploaderService
}

func NewChunkedUploaderHandler(service *ChunkedUploaderService) *ChunkedUploaderHandler {
	return &ChunkedUploaderHandler{service: service}
}

type CreateUploadRequest struct {
	FileSize int64 `json:"file_size"`
}

// CreateUploadHandler creates a new upload with a given file size and returns an uploadId.
func (c *ChunkedUploaderHandler) CreateUploadHandler(w http.ResponseWriter, r *http.Request) {
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

	uploadId, err := c.service.CreateUpload(req.FileSize)
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

	h, err := c.service.UploadChunk(uploadId, requestFile, rangeStart, computeHash)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to upload chunk: "+err.Error())
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

	err = c.service.FinishUpload(uploadId, expectedChecksum)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to verify upload: "+err.Error())
		return
	}

	pendingPath := getUploadFilePath(uploadId)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"path": pendingPath})
}

// OpenUploadedFileHandler opens an uploaded file with a given uploadId and returns a file handle.
func (c *ChunkedUploaderHandler) OpenUploadedFileHandler(uploadId string) (io.ReadCloser, error) {
	return c.service.OpenUploadedFile(uploadId)
}

type RenameUploadedFileRequest struct {
	Path string
}

// RenameUploadedFileHandler renames an uploaded file to a given path.
func (c *ChunkedUploaderHandler) RenameUploadedFileHandler(w http.ResponseWriter, r *http.Request) {
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

	err = c.service.RenameUploadedFile(uploadId, req.Path)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to rename uploaded file: "+err.Error())
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

func getUploadFilePath(uploadId string) string {
	return filepath.Join(".pending", uploadId)
}

func getUploadPath() string {
	return ".pending"
}
