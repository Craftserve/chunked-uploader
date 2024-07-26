package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type InitResponse struct {
	UploadID string `json:"upload_id"`
}

type FinishResponse struct {
	Path string `json:"path"`
}

type Client struct {
	DoRequest func(req *http.Request) (*http.Response, error)
	Endpoint  string
	ChunkSize int64
	UploadId  *string
}

func (c *Client) Upload(ctx context.Context, fileReader io.ReadCloser) (path string, err error) {
	err = c.initUpload(ctx)
	if err != nil {
		return "", err
	}
	chunkUrl := fmt.Sprintf("%s/%s/upload", c.Endpoint, *c.UploadId)

	hash := sha256.New()
	hashingReader := io.TeeReader(fileReader, hash)

	for {
		chunkReader := io.LimitedReader{R: hashingReader, N: c.ChunkSize}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, chunkUrl, &chunkReader)
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
		req.Header.Set("Content-Type", "application/octet-stream")

		res, err := c.DoRequest(req)
		if err != nil {
			return "", fmt.Errorf("failed to upload chunk %w", err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf("failed to upload chunk %s", getJsonError(res.Body))
		}

		if chunkReader.N == c.ChunkSize {
			break
		}

	}

	path, err = c.finishUpload(ctx, hex.EncodeToString(hash.Sum(nil)))
	if err != nil {
		return "", err
	}

	return path, nil
}

func (c *Client) initUpload(ctx context.Context) error {
	var args = struct {
		FileSize *int64 `json:"file_size"`
	}{
		FileSize: nil,
	}

	var resp InitResponse
	err := c.sendJsonRequest(ctx, c.Endpoint+"/init", args, http.StatusCreated, &resp)
	if err != nil {
		return err
	}
	c.UploadId = &resp.UploadID
	return nil
}

func (c *Client) finishUpload(ctx context.Context, hash string) (string, error) {
	var args = struct {
		Checksum string `json:"checksum"`
	}{
		Checksum: hash,
	}

	finishUrl := fmt.Sprintf("%s/%s/finish", c.Endpoint, *c.UploadId)

	var resp FinishResponse
	err := c.sendJsonRequest(ctx, finishUrl, &args, http.StatusOK, &resp)
	if err != nil {
		return "", err
	}

	return resp.Path, nil
}

func (c *Client) sendJsonRequest(ctx context.Context, url string, args interface{}, expectedStatus int, response interface{}) error {
	var reqBody *bytes.Buffer
	if args != nil {
		reqBody = &bytes.Buffer{}
		err := json.NewEncoder(reqBody).Encode(args)
		if err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.DoRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("server error: %s", resp.Status)
	}

	err = json.NewDecoder(resp.Body).Decode(response)
	if err != nil {
		return fmt.Errorf("could not decode response %w", err)
	}
	return nil
}

func getJsonError(body io.Reader) string {
	var response map[string]interface{}
	err := json.NewDecoder(body).Decode(&response)
	if err != nil {
		return err.Error()
	}

	return response["error"].(string)
}
