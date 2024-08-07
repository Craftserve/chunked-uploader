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
	rootFs := afero.NewBasePathFs(fs, ".") // just to show that you can use base path fs
	service := chunkeduploader.NewChunkedUploaderService(rootFs, nil)
	handlers := chunkeduploader.NewChunkedUploaderHandler(service)

	r := mux.NewRouter()

	r.HandleFunc("/init", handlers.CreateUploadHandler).Methods("POST")
	r.HandleFunc("/{upload_id}/upload", handlers.UploadChunkHandler).Methods("POST")
	r.HandleFunc("/{upload_id}/finish", handlers.FinishUploadHandler).Methods("POST")

	fmt.Println("Server is running on port 8081")
	err := http.ListenAndServe(":8081", r)
	if err != nil {
		fmt.Println("Error starting the server:", err)
	}
}
