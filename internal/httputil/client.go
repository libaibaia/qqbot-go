package httputil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultTimeout      = 30 * time.Second
	FileUploadTimeout   = 120 * time.Second
	TokenEndpoint       = "https://bots.qq.com/app/getAppAccessToken"
	BaseURL             = "https://api.sgroup.qq.com"
	UserAgent           = "QQBotGo/1.0"
)

// APIError represents an error from the QQ Bot API.
type APIError struct {
	StatusCode int
	Path       string
	BizCode    int
	BizMessage string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("qqbot api error: path=%s status=%d bizcode=%d msg=%s",
		e.Path, e.StatusCode, e.BizCode, e.BizMessage)
}

// DoJSON performs an HTTP request with JSON body and decodes the JSON response.
func DoJSON(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Check for HTML error responses (CDN errors)
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 400 && strings.Contains(ct, "text/html") {
		return &APIError{
			StatusCode: resp.StatusCode,
			Path:       url,
			BizMessage: fmt.Sprintf("CDN error %d", resp.StatusCode),
		}
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
		}
	}

	return nil
}

// DoRequest performs an HTTP request and returns the raw response body.
func DoRequest(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body any) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}
