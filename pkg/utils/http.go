package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// HTTPClient represents a configured HTTP client
type HTTPClient struct {
	client *http.Client
	logger logger.Logger
}

// NewHTTPClient creates a new HTTP client with default configuration
func NewHTTPClient(timeout time.Duration, log logger.Logger) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logger: log,
	}
}

// Get performs a GET request
func (h *HTTPClient) Get(ctx context.Context, url string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	// Add headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	h.logger.Debug("Making HTTP GET request",
		logger.String("url", url),
		logger.Any("headers", headers),
	)

	start := time.Now()
	resp, err := h.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		h.logger.Error("HTTP GET request failed",
			logger.String("url", url),
			logger.Duration("duration", duration),
			logger.ErrorField(err),
		)
		return nil, err
	}

	h.logger.Debug("HTTP GET request completed",
		logger.String("url", url),
		logger.Int("status", resp.StatusCode),
		logger.Duration("duration", duration),
	)

	return resp, nil
}

// PostJSON performs a POST request with JSON body
func (h *HTTPClient) PostJSON(ctx context.Context, url string, body interface{}, headers map[string]string) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	// Set content type
	req.Header.Set("Content-Type", "application/json")

	// Add additional headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	h.logger.Debug("Making HTTP POST request",
		logger.String("url", url),
		logger.Any("headers", headers),
	)

	start := time.Now()
	resp, err := h.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		h.logger.Error("HTTP POST request failed",
			logger.String("url", url),
			logger.Duration("duration", duration),
			logger.ErrorField(err),
		)
		return nil, err
	}

	h.logger.Debug("HTTP POST request completed",
		logger.String("url", url),
		logger.Int("status", resp.StatusCode),
		logger.Duration("duration", duration),
	)

	return resp, nil
}

// JSONResponse writes a JSON response
func JSONResponse(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if data != nil {
		return json.NewEncoder(w).Encode(data)
	}

	return nil
}

// ErrorResponse writes an error response
func ErrorResponse(w http.ResponseWriter, status int, message string) error {
	return JSONResponse(w, status, map[string]string{
		"error":   http.StatusText(status),
		"message": message,
	})
}

// SuccessResponse writes a success response
func SuccessResponse(w http.ResponseWriter, data interface{}) error {
	return JSONResponse(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    data,
	})
}