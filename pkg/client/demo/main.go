package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	client "github.com/Craftserve/chunked-uploader/pkg/client"
)

func main() {
	httpClient := http.Client{}

	client := client.Client{
		Endpoint:  "http://localhost:8081",
		ChunkSize: 500,
		DoRequest: httpClient.Do,
	}

	file, err := os.Open("test2.zip")
	if err != nil {
		panic(err)
	}

	defer file.Close()

	ctx := context.Background()

	path, err := client.Upload(ctx, file)

	if err != nil {
		panic(err)
	}

	fmt.Println("Uploaded file path:", path)
}
