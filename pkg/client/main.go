package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

type ClientEndpoints struct {
	Init   string
	Upload string
	Finish string
}

type ClientConfig struct {
	MaxChunkSize int64
}

type InitResponse struct {
	UploadID string `json:"upload_id"`
}

type FinishResponse struct {
	Path string `json:"path"`
}

type Client struct {
	HttpClient *http.Client
	Endpoints  ClientEndpoints
	Config     ClientConfig
	Cookies    []*http.Cookie
}

func NewClient(endpoints ClientEndpoints, cookies []*http.Cookie, config ClientConfig) *Client {
	return &Client{
		HttpClient: &http.Client{},
		Endpoints:  endpoints,
		Config:     config,
		Cookies:    cookies,
	}
}

func (c *Client) Upload(ctx context.Context, fileReader io.ReadCloser) (path string, err error) {
	upload_id, err := c.initUpload(ctx)

	c.generateEndpoints(upload_id)

	lastByte := int64(0)
	hash := sha256.New()

	hashWriter := io.TeeReader(fileReader, hash)

	for {
		limitedReader := io.LimitedReader{R: hashWriter, N: c.Config.MaxChunkSize}

		buffer := &bytes.Buffer{}
		_, err := buffer.ReadFrom(&limitedReader)
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("failed to read chunk %w", err)
		}

		chunk := buffer.Bytes()

		if len(chunk) == 0 {
			break
		}

		bodyFormData := &bytes.Buffer{}
		writer := multipart.NewWriter(bodyFormData)
		part, err := writer.CreateFormFile("file", "file")
		if err != nil {
			return "", fmt.Errorf("failed to create form file %w", err)
		}

		n, err := part.Write(chunk)
		if err != nil {
			return "", fmt.Errorf("failed to write part %w", err)
		}

		err = writer.Close()
		if err != nil {
			return "", fmt.Errorf("failed to close writer %w", err)
		}

		res, err := c.sendChunkRequest(ctx, c.Endpoints.Upload, bytes.NewReader(chunk), lastByte, lastByte+int64(n)-1, true)
		if err != nil {
			return "", fmt.Errorf("failed to upload chunk %w", err)
		}

		defer res.Body.Close()

		lastByte += int64(n)

		if res.StatusCode != http.StatusCreated {
			return "", fmt.Errorf("failed to upload chunk %s", getJsonError(res.Body))
		}
	}

	path, err = c.finishUpload(ctx, hex.EncodeToString(hash.Sum(nil)))
	if err != nil {
		return "", err
	}

	return path, nil
}

func (c *Client) generateEndpoints(uploadId string) {
	c.Endpoints.Upload = strings.ReplaceAll(c.Endpoints.Upload, "{upload_id}", uploadId)
	c.Endpoints.Finish = strings.ReplaceAll(c.Endpoints.Finish, "{upload_id}", uploadId)
}

func (c *Client) sendJsonRequest(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range c.Cookies {
		req.AddCookie(cookie)
	}

	return c.HttpClient.Do(req)
}

func (c *Client) sendChunkRequest(ctx context.Context, url string, chunk io.Reader, rangeStart int64, rangeEnd int64, computeHash bool) (*http.Response, error) {
	bodyFormData := &bytes.Buffer{}
	writer := multipart.NewWriter(bodyFormData)
	part, err := writer.CreateFormFile("file", "file")
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(part, chunk)
	if err != nil {
		return nil, err
	}

	err = writer.Close()

	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyFormData)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	for _, cookie := range c.Cookies {
		req.AddCookie(cookie)
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd))

	return c.HttpClient.Do(req)
}

func getJsonError(body io.ReadCloser) error {
	var errorResponse struct {
		Error string `json:"error"`
	}

	err := json.NewDecoder(body).Decode(&errorResponse)
	if err != nil {
		return fmt.Errorf("could not decode error response %w", err)
	}

	return fmt.Errorf("server error: %s", errorResponse.Error)
}

func (c *Client) initUpload(ctx context.Context) (string, error) {
	res, err := c.sendJsonRequest(ctx, c.Endpoints.Init, map[string]int64{"file_size": -1})
	if err != nil {
		return "", err
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("failed to create upload %s", getJsonError(res.Body))
	}

	var response InitResponse
	err = json.NewDecoder(res.Body).Decode(&response)
	if err != nil {
		return "", fmt.Errorf("could not decode request %w", err)
	}

	return response.UploadID, nil
}

func (c *Client) finishUpload(ctx context.Context, hash string) (string, error) {
	res, err := c.sendJsonRequest(ctx, c.Endpoints.Finish, map[string]string{"checksum": hash})
	if err != nil {
		return "", err
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("failed to finish upload %s", getJsonError(res.Body))
	}

	var response FinishResponse
	err = json.NewDecoder(res.Body).Decode(&response)
	if err != nil {
		return "", fmt.Errorf("could not decode request %w", err)
	}

	return response.Path, nil
}
