package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/circuitbreaker"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/errors"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
	commonmiddleware "github.com/thesoftwaremasons/scholarplatforms-common/pkg/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Client is an enhanced HTTP client with circuit breaker and tracing
type Client struct {
	httpClient     *http.Client
	circuitBreaker *circuitbreaker.CircuitBreaker
	logger         logger.Logger
	baseURL        string
	defaultHeaders map[string]string
	timeout        time.Duration
}

// Config holds HTTP client configuration
type Config struct {
	BaseURL           string
	Timeout           time.Duration
	CircuitBreakerCfg *circuitbreaker.Config
	DefaultHeaders    map[string]string
	EnableTracing     bool
	Transport         http.RoundTripper
}

// NewClient creates a new enhanced HTTP client
func NewClient(cfg Config, log logger.Logger, cbManager *circuitbreaker.CircuitBreakerManager) *Client {
	// Create HTTP client with optional tracing
	var transport http.RoundTripper = cfg.Transport
	if transport == nil {
		transport = &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		}
	}

	// Wrap with OpenTelemetry if tracing enabled
	if cfg.EnableTracing {
		transport = otelhttp.NewTransport(transport)
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}

	// Get or create circuit breaker
	var cb *circuitbreaker.CircuitBreaker
	if cbManager != nil {
		cbName := fmt.Sprintf("http-client-%s", cfg.BaseURL)
		cb = cbManager.GetOrCreate(cbName, cfg.CircuitBreakerCfg)
	}

	return &Client{
		httpClient:     httpClient,
		circuitBreaker: cb,
		logger:         log,
		baseURL:        cfg.BaseURL,
		defaultHeaders: cfg.DefaultHeaders,
		timeout:        cfg.Timeout,
	}
}

// Request represents an HTTP request
type Request struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    interface{}
	Query   map[string]string
}

// Response represents an HTTP response
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// Do executes an HTTP request
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	// Build URL
	url := c.baseURL + req.Path
	if len(req.Query) > 0 {
		url += "?"
		first := true
		for k, v := range req.Query {
			if !first {
				url += "&"
			}
			url += fmt.Sprintf("%s=%s", k, v)
			first = false
		}
	}

	// Marshal body if present
	var bodyReader io.Reader
	if req.Body != nil {
		bodyBytes, err := json.Marshal(req.Body)
		if err != nil {
			return nil, errors.NewInternalError("failed to marshal request body").WithCause(err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, bodyReader)
	if err != nil {
		return nil, errors.NewInternalError("failed to create request").WithCause(err)
	}

	// Add default headers
	for k, v := range c.defaultHeaders {
		httpReq.Header.Set(k, v)
	}

	// Add request-specific headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	applyContextHeaders(ctx, httpReq)

	// Set Content-Type if body is present
	if req.Body != nil && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Execute with circuit breaker if available
	var resp *http.Response
	if c.circuitBreaker != nil {
		result, err := c.circuitBreaker.ExecuteContext(ctx, func(ctx context.Context) (interface{}, error) {
			return c.httpClient.Do(httpReq)
		})

		if err != nil {
			c.logger.Error("HTTP request failed (circuit breaker)",
				logger.ErrorField(err),
				logger.String("url", url),
				logger.String("method", req.Method),
			)
			return nil, err
		}

		resp = result.(*http.Response)
	} else {
		resp, err = c.httpClient.Do(httpReq)
		if err != nil {
			c.logger.Error("HTTP request failed",
				logger.ErrorField(err),
				logger.String("url", url),
				logger.String("method", req.Method),
			)
			return nil, errors.NewServiceUnavailableError(c.baseURL).WithCause(err)
		}
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.NewInternalError("failed to read response body").WithCause(err)
	}

	// Log request
	c.logger.Debug("HTTP request completed",
		logger.String("url", url),
		logger.String("method", req.Method),
		logger.Int("status_code", resp.StatusCode),
	)

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		return &Response{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       respBody,
		}, c.httpErrorToAppError(resp.StatusCode, respBody)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       respBody,
	}, nil
}

func applyContextHeaders(ctx context.Context, req *http.Request) {
	if tenantID := commonmiddleware.GetTenantID(ctx); tenantID != "" && req.Header.Get("X-Tenant-ID") == "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	if schoolID := commonmiddleware.GetSchoolID(ctx); schoolID != "" && req.Header.Get("X-School-ID") == "" {
		req.Header.Set("X-School-ID", schoolID)
	}
	if authToken := commonmiddleware.GetAuthToken(ctx); authToken != "" && req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", authToken)
	}
}

// Get executes a GET request
func (c *Client) Get(ctx context.Context, path string, query map[string]string) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodGet,
		Path:   path,
		Query:  query,
	})
}

// Post executes a POST request
func (c *Client) Post(ctx context.Context, path string, body interface{}) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
	})
}

// Put executes a PUT request
func (c *Client) Put(ctx context.Context, path string, body interface{}) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodPut,
		Path:   path,
		Body:   body,
	})
}

// Delete executes a DELETE request
func (c *Client) Delete(ctx context.Context, path string) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodDelete,
		Path:   path,
	})
}

// Patch executes a PATCH request
func (c *Client) Patch(ctx context.Context, path string, body interface{}) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodPatch,
		Path:   path,
		Body:   body,
	})
}

// DecodeJSON decodes the response body into the target
func (r *Response) DecodeJSON(target interface{}) error {
	if err := json.Unmarshal(r.Body, target); err != nil {
		return errors.NewInternalError("failed to decode JSON response").WithCause(err)
	}
	return nil
}

// httpErrorToAppError converts HTTP errors to application errors
func (c *Client) httpErrorToAppError(statusCode int, body []byte) error {
	// Try to parse error response
	var errorResp struct {
		Error struct {
			Code    string                 `json:"code"`
			Message string                 `json:"message"`
			Details map[string]interface{} `json:"details"`
		} `json:"error"`
	}

	message := fmt.Sprintf("HTTP %d error", statusCode)
	if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error.Message != "" {
		message = errorResp.Error.Message
	}

	// Map HTTP status to application error
	switch statusCode {
	case http.StatusBadRequest:
		return errors.NewValidationError(message)
	case http.StatusUnauthorized:
		return errors.NewUnauthorizedError(message)
	case http.StatusForbidden:
		return errors.NewForbiddenError(message)
	case http.StatusNotFound:
		return errors.NewNotFoundError("resource", "")
	case http.StatusConflict:
		return errors.NewConflictError(message)
	case http.StatusTooManyRequests:
		return errors.NewRateLimitExceededError(0, "")
	case http.StatusServiceUnavailable:
		return errors.NewServiceUnavailableError(c.baseURL)
	default:
		return errors.NewInternalError(message)
	}
}

// GetCircuitBreakerState returns the current circuit breaker state
func (c *Client) GetCircuitBreakerState() string {
	if c.circuitBreaker != nil {
		return c.circuitBreaker.State().String()
	}
	return "N/A"
}

// SetHeader sets a default header for all requests
func (c *Client) SetHeader(key, value string) {
	if c.defaultHeaders == nil {
		c.defaultHeaders = make(map[string]string)
	}
	c.defaultHeaders[key] = value
}

// SetAuthToken sets the Authorization header
func (c *Client) SetAuthToken(token string) {
	c.SetHeader("Authorization", "Bearer "+token)
}
