package main

import (
	"context"
	"fmt"
	"io"
	"os"

	client "github.com/Craftserve/chunked-uploader/pkg/client"
)

func main() {
	client := client.NewClient(client.ClientEndpoints{
		Init:   "http://localhost:8081/init",
		Upload: "http://localhost:8081/upload/{upload_id}",
		Finish: "http://localhost:8081/finish/{upload_id}",
	}, nil, client.ClientConfig{
		MaxChunkSize: 500,
	})

	file, err := os.Open("test2.zip")
	if err != nil {
		panic(err)
	}

	defer file.Close()

	fileReader := io.ReadCloser(file)

	defer fileReader.Close()

	ctx := context.Background()

	path, err := client.Upload(ctx, fileReader)

	if err != nil {
		panic(err)
	}

	fmt.Println("Uploaded file path:", path)
}
