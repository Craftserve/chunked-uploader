package main

import (
	"fmt"
	"net/http"

	chunkeduploader "github.com/Craftserve/chunked-uploader"
	"github.com/gorilla/mux"
	"github.com/spf13/afero"
)

func main() {
	fs := afero.NewOsFs()
	uploader := chunkeduploader.NewChunkedUploaderService(fs, ".")

	r := mux.NewRouter()
	r.HandleFunc("/init", uploader.CreateUploadHandler).Methods("POST")
	r.HandleFunc("/upload/{upload_id}", uploader.UploadChunkHandler).Methods("POST")
	r.HandleFunc("/finish/{upload_id}", uploader.FinishUploadHandler).Methods("POST")
	r.HandleFunc("/rename/{upload_id}", uploader.RenameUploadedFileHandler).Methods("POST")

	fmt.Println("Server is running on port 8080")
	err := http.ListenAndServe(":8080", r)
	if err != nil {
		fmt.Println("Error starting the server:", err)
	}
}
