package client

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"
)

type ClientEndpoints struct {
	Init   string
	Upload string
	Finish string
}

type ClientConfig struct {
	Cookies []*http.Cookie
}

type InitResponse struct {
	UploadID string `json:"upload_id"`
}

type FinishResponse struct {
	Path string `json:"path"`
}

type Client struct {
	Endpoints ClientEndpoints
	Config    ClientConfig
}

func NewClient(endpoints ClientEndpoints, config *ClientConfig) *Client {
	return &Client{
		Endpoints: endpoints,
		Config:    *config,
	}
}

func (c *Client) Upload(file os.File, chunkSize int64) (path string, err error) {
	fileInfo, err := file.Stat()
	if err != nil {
		return "", err
	}

	body := bytes.NewBuffer([]byte(fmt.Sprintf(`{"file_size": %d}`, fileInfo.Size())))

	req, err := http.NewRequest("POST", c.Endpoints.Init, body)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range c.Config.Cookies {
		req.AddCookie(cookie)
	}

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("failed to create upload")
	}

	var response InitResponse
	err = json.NewDecoder(res.Body).Decode(&response)
	if err != nil {
		return "", fmt.Errorf("could not decode request %w", err)
	}

	upload_id := response.UploadID

	c.Endpoints.Upload = strings.Replace(c.Endpoints.Upload, "{upload_id}", upload_id, 1)
	c.Endpoints.Finish = strings.Replace(c.Endpoints.Finish, "{upload_id}", upload_id, 1)

	noOfChunks := math.Ceil(float64(fileInfo.Size() / chunkSize))

	var wg sync.WaitGroup
	errc := make(chan error, 10)

	for i := 0; i < int(noOfChunks); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			start := int64(i) * chunkSize
			end := int(math.Min(float64(fileInfo.Size()), float64(start+chunkSize)))
			file.Seek(int64(start), 0)
			chunk := make([]byte, int64(end)-start)
			_, err := file.Read(chunk)
			if err != nil {
				errc <- err
				return
			}

			bodyFormData := &bytes.Buffer{}
			writer := multipart.NewWriter(bodyFormData)
			part, err := writer.CreateFormFile("file", "file")
			if err != nil {
				errc <- err
				return
			}

			part.Write(chunk)
			err = writer.Close()
			if err != nil {
				errc <- err
				return
			}

			req, err := http.NewRequest("POST", c.Endpoints.Upload, bodyFormData)
			if err != nil {
				errc <- err
				return
			}

			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end-1))
			for _, cookie := range c.Config.Cookies {
				req.AddCookie(cookie)
			}
			client := &http.Client{}
			res, err := client.Do(req)
			if err != nil {
				errc <- err
				return
			}

			defer res.Body.Close()

			if res.StatusCode != http.StatusCreated {
				errc <- fmt.Errorf("failed to upload chunk")
			}
		}(i)
	}

	wg.Wait()

	close(errc)

	for err := range errc {
		if err != nil {
			return "", err
		}
	}

	file.Seek(0, 0)

	hash := sha256.New()
	if _, err := io.Copy(hash, &file); err != nil {
		return "", err
	}

	hexHash := hex.EncodeToString(hash.Sum(nil))
	body = bytes.NewBuffer([]byte(fmt.Sprintf(`{"checksum": "%x"}`, hexHash)))

	req, err = http.NewRequest("POST", c.Endpoints.Finish, body)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range c.Config.Cookies {
		req.AddCookie(cookie)
	}

	client = &http.Client{}

	res, err = client.Do(req)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("failed to finish upload")
	}

	var finishResponse FinishResponse
	err = json.NewDecoder(res.Body).Decode(&finishResponse)
	if err != nil {
		return "", fmt.Errorf("could not decode request %w", err)
	}

	return finishResponse.Path, nil
}
