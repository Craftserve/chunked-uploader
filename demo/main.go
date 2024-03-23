package main

import (
	"fmt"
	"net/http"

	"github.com/Craftserve/chunked-uploader"
	"github.com/Craftserve/chunked-uploader/pkg/handlers"
	"github.com/gorilla/mux"
	"github.com/spf13/afero"
)

func main() {
	fs := afero.NewOsFs()
	rootFs := afero.NewBasePathFs(fs, ".") // just to show that you can use base path fs
	service := chunkeduploader.NewService(rootFs)
	uploaderHandler := handlers.NewChunkedUploaderHandler(service)

	r := mux.NewRouter()

	r.HandleFunc("/init", uploaderHandler.CreateUploadHandler).Methods("POST")
	r.HandleFunc("/upload/{upload_id}", uploaderHandler.UploadChunkHandler).Methods("POST")
	r.HandleFunc("/finish/{upload_id}", uploaderHandler.FinishUploadHandler).Methods("POST")
	r.HandleFunc("/rename/{upload_id}", uploaderHandler.RenameUploadedFileHandler).Methods("POST")

	fmt.Println("Server is running on port 8081")
	err := http.ListenAndServe(":8081", r)
	if err != nil {
		fmt.Println("Error starting the server:", err)
	}
}
