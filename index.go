package chunkeduploader

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Craftserve/chunked-uploader/pkg/checksum"
	"github.com/Craftserve/chunked-uploader/pkg/files"
	"github.com/Craftserve/chunked-uploader/pkg/logger"
	"github.com/spf13/afero"
)

type ChunkedUploaderService struct {
	fs afero.Fs
	mu sync.Mutex
}

func NewChunkedUploaderService(fs afero.Fs) *ChunkedUploaderService {
	return &ChunkedUploaderService{fs: fs}
}

func (c *ChunkedUploaderService) CreatePendingFile(uploadId string, filename string) (err error) {
	tempPath := files.GetPendingFilePath(uploadId, filename)
	file, err := files.CreateFile(c.fs, tempPath)

	if err != nil {
		return fmt.Errorf("Failed to create temp file: "+err.Error(), http.StatusInternalServerError)
	}

	defer file.Close()

	return err
}

func (c *ChunkedUploaderService) CreateMetadataFile(uploadId string, fileSize int, path string, checksum string) (err error) {
	metadataPath := files.GetMetadataFilePath(uploadId)

	lastAttempt := time.Now().Unix()

	metadata := map[string]interface{}{
		"upload_id":   uploadId,
		"last_attemp": lastAttempt,
		"total_size":  fileSize,
		"path":        path,
		"checksum":    checksum,
	}

	file, err := files.CreateFile(c.fs, metadataPath)
	if err != nil {
		return fmt.Errorf("Failed to create metadata file, "+err.Error(), http.StatusInternalServerError)
	}

	defer file.Close()

	err = files.LockFile(file)
	if err != nil {
		return fmt.Errorf("Failed to lock file, "+err.Error(), http.StatusInternalServerError)
	}

	encoder := json.NewEncoder(file)
	if err = encoder.Encode(metadata); err != nil {
		return fmt.Errorf("Failed to write metadata"+err.Error(), http.StatusInternalServerError)
	}

	err = files.UnlockFile(file)
	if err != nil {
		return fmt.Errorf("Failed to unlock file, "+err.Error(), http.StatusInternalServerError)
	}

	return nil
}

func (c *ChunkedUploaderService) ClearPendingData(uploadId string) error {
	pendingPath := files.GetPendingPath(uploadId)
	err := c.fs.RemoveAll(pendingPath)
	if err != nil {
		return fmt.Errorf("failed to remove pending directory, "+err.Error(), http.StatusInternalServerError)
	}

	return nil
}

func (c *ChunkedUploaderService) VerifyUpload(uploadId string, filename string, expectedSize int64, expectedChecksum string) error {
	pendingPath := files.GetPendingFilePath(uploadId, filename)

	checksum, err := checksum.ComputeChecksum(c.fs, pendingPath)
	if err != nil {
		return fmt.Errorf("Failed to compute checksum, " + err.Error())
	}

	if checksum != expectedChecksum {
		return fmt.Errorf("checksum does not match expected checksum")
	}

	return nil
}

func (c *ChunkedUploaderService) InitUploadHandler(w http.ResponseWriter, r *http.Request) {
	uploadId := r.URL.Query().Get("upload_id")
	if uploadId == "" {
		http.Error(w, "upload_id is required", http.StatusBadRequest)
		return
	}

	fileSizeStr := r.URL.Query().Get("file_size")
	if fileSizeStr == "" {
		http.Error(w, "file_size is required", http.StatusBadRequest)
		return
	}

	fileSize, err := strconv.ParseInt(fileSizeStr, 10, 64)
	if err != nil {
		http.Error(w, "file_size must be an integer", http.StatusBadRequest)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	checksum := r.URL.Query().Get("checksum")
	if path == "" {
		http.Error(w, "checksum is required", http.StatusBadRequest)
		return
	}

	err = c.CreateMetadataFile(uploadId, int(fileSize), path, checksum)
	if err != nil {
		http.Error(w, "Failed white creating metadata file, "+err.Error(), http.StatusInternalServerError)
	}

	filename := filepath.Base(path)
	err = c.CreatePendingFile(uploadId, filename)
	if err != nil {
		http.Error(w, "Failed white creating temp file, "+err.Error(), http.StatusInternalServerError)
	}
}

func (c *ChunkedUploaderService) UploadChunkHandler(w http.ResponseWriter, r *http.Request) {
	uploadId := r.URL.Query().Get("upload_id")
	if uploadId == "" {
		http.Error(w, "upload_id is required", http.StatusBadRequest)
		return
	}

	metadataPath := files.GetMetadataFilePath(uploadId)

	rangeStartStr := r.URL.Query().Get("range_start")
	if rangeStartStr == "" {
		http.Error(w, "range_start is required", http.StatusBadRequest)
		return
	}

	rangeStart, err := strconv.ParseInt(rangeStartStr, 10, 64)
	if err != nil {
		http.Error(w, "range_start must be an integer", http.StatusBadRequest)
		return
	}

	rangeEndStr := r.URL.Query().Get("range_end")
	if rangeEndStr == "" {
		http.Error(w, "range_end is required", http.StatusBadRequest)
		return
	}

	rangeEnd, err := strconv.ParseInt(rangeEndStr, 10, 64)
	if err != nil {

		http.Error(w, "range_end must be an integer", http.StatusBadRequest)
		return
	}

	if rangeStart > rangeEnd {
		http.Error(w, "range_start must be less than or equal to range_end", http.StatusBadRequest)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	filename := filepath.Base(path)

	tempPath := files.GetPendingFilePath(uploadId, filename)
	file, err := files.OpenFile(c.fs, tempPath, os.O_WRONLY, files.OnlyRead)
	if err != nil {

		http.Error(w, "Failed to open temp file, "+err.Error(), http.StatusInternalServerError)
		return
	}

	defer file.Close()

	metadataFile, err := files.OpenFile(c.fs, metadataPath, os.O_RDONLY, files.OnlyRead)
	if err != nil {

		http.Error(w, "Failed to open metadata file, "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = file.Seek(rangeStart, 0)
	if err != nil {

		http.Error(w, "Failed to seek to range_start, "+err.Error(), http.StatusInternalServerError)
		return
	}

	buf := make([]byte, rangeEnd-rangeStart)

	err = r.ParseMultipartForm(100 << 20) // 100 MB max memory
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	requestFile, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Could not get file", http.StatusInternalServerError)
		return
	}

	defer requestFile.Close()

	if err != nil {
		http.Error(w, "Failed to read request body, "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = requestFile.Read(buf)
	if err != nil {

		http.Error(w, "Could not read file"+err.Error(), http.StatusInternalServerError)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	_, err = file.Write(buf)
	if err != nil {

		http.Error(w, "Failed to write to file, "+err.Error(), http.StatusInternalServerError)
		return
	}

	decoder := json.NewDecoder(metadataFile)
	var metadata map[string]interface{}
	err = decoder.Decode(&metadata)
	if err != nil {
		http.Error(w, "Failed to read metadata file, "+err.Error(), http.StatusInternalServerError)
		return
	}

	metadata["last_attempt"] = time.Now().Unix()

	metadataFile.Seek(0, 0)
	encoder := json.NewEncoder(metadataFile)
	err = encoder.Encode(metadata)
	if err != nil {
		http.Error(w, "Failed to write metadata file, "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func (c *ChunkedUploaderService) FinishUploadHandler(w http.ResponseWriter, r *http.Request) {
	uploadId := r.URL.Query().Get("upload_id")
	if uploadId == "" {
		http.Error(w, "upload_id is required", http.StatusBadRequest)
		return
	}

	metadataPath := files.GetMetadataFilePath(uploadId)
	file, err := c.fs.Open(metadataPath)
	if err != nil {
		http.Error(w, "Failed to open metadata file, "+err.Error(), http.StatusInternalServerError)
		return
	}

	defer file.Close()

	err = files.LockFile(file)

	if err != nil {
		http.Error(w, "Failed to lock metadata file, "+err.Error(), http.StatusInternalServerError)
		return
	}

	decoder := json.NewDecoder(file)
	var metadata map[string]interface{}
	err = decoder.Decode(&metadata)
	if err != nil {
		http.Error(w, "Failed to read metadata file, "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = files.UnlockFile(file)

	if err != nil {
		http.Error(w, "Failed to unlock metadata file, "+err.Error(), http.StatusInternalServerError)
		return
	}

	path := metadata["path"].(string)
	filename := filepath.Base(path)
	fileSize := int64(metadata["total_size"].(float64))
	expectedChecksum := metadata["checksum"].(string)

	err = c.VerifyUpload(uploadId, filename, fileSize, expectedChecksum)
	if err != nil {
		err = c.ClearPendingData(uploadId)
		if err != nil {
			logger.Log("Failed to clear pending data, " + err.Error())
		}

		http.Error(w, "Failed to verify upload, "+err.Error(), http.StatusInternalServerError)
		return
	}

	pendingPath := files.GetPendingFilePath(uploadId, filename)
	basePath := filepath.Dir(path)

	err = c.fs.MkdirAll(basePath, files.StandardAccess)
	if err != nil {
		http.Error(w, "Failed to create directory, "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = c.fs.Rename(pendingPath, path)
	if err != nil {
		http.Error(w, "Failed to move file, "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = c.ClearPendingData(uploadId)
	if err != nil {
		logger.Log("Failed to clear pending data, " + err.Error())
	}

	w.WriteHeader(http.StatusCreated)
}
