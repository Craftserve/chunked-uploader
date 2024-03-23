package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Craftserve/chunked-uploader"
	"github.com/gorilla/mux"
)

type ChunkedUploaderHandler struct {
	service *chunkeduploader.Service
}

func NewChunkedUploaderHandler(service *chunkeduploader.Service) *ChunkedUploaderHandler {
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

	path, err := c.service.FinishUpload(uploadId, expectedChecksum)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to verify upload: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"path": path})
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
