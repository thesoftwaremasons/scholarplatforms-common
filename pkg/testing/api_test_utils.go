// Package testing provides API testing utilities
package testing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// APITestClient provides utilities for testing HTTP APIs
type APITestClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Logger     logger.Logger
	Headers    map[string]string
	Timeout    time.Duration
}

// APITestRequest represents a test API request
type APITestRequest struct {
	Method      string            `json:"method"`
	Endpoint    string            `json:"endpoint"`
	Headers     map[string]string `json:"headers"`
	Body        interface{}       `json:"body"`
	QueryParams map[string]string `json:"query_params"`
}

// APITestResponse represents an API test response
type APITestResponse struct {
	StatusCode int                    `json:"status_code"`
	Headers    map[string][]string    `json:"headers"`
	Body       string                 `json:"body"`
	JSON       map[string]interface{} `json:"json,omitempty"`
	Duration   time.Duration          `json:"duration"`
}

// APITestAssertion represents an assertion for API testing
type APITestAssertion struct {
	Field       string      `json:"field"`
	Operator    string      `json:"operator"`
	Expected    interface{} `json:"expected"`
	Description string      `json:"description"`
}

// NewAPITestClient creates a new API test client
func NewAPITestClient(baseURL string, logger logger.Logger) *APITestClient {
	return &APITestClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		Logger:  logger,
		Headers: make(map[string]string),
		Timeout: 30 * time.Second,
	}
}

// SetHeader sets a default header for all requests
func (client *APITestClient) SetHeader(key, value string) {
	client.Headers[key] = value
}

// SetTimeout sets the request timeout
func (client *APITestClient) SetTimeout(timeout time.Duration) {
	client.Timeout = timeout
	client.HTTPClient.Timeout = timeout
}

// SetAuthToken sets the authorization header
func (client *APITestClient) SetAuthToken(token string) {
	client.SetHeader("Authorization", "Bearer "+token)
}

// Request performs an HTTP request and returns the response
func (client *APITestClient) Request(req APITestRequest) (*APITestResponse, error) {
	start := time.Now()
	
	// Build URL with query parameters
	url := client.BaseURL + req.Endpoint
	if len(req.QueryParams) > 0 {
		query := make([]string, 0, len(req.QueryParams))
		for key, value := range req.QueryParams {
			query = append(query, fmt.Sprintf("%s=%s", key, value))
		}
		url += "?" + strings.Join(query, "&")
	}
	
	// Prepare request body
	var bodyReader io.Reader
	if req.Body != nil {
		bodyBytes, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}
	
	// Create HTTP request
	httpReq, err := http.NewRequest(req.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	
	// Set headers
	for key, value := range client.Headers {
		httpReq.Header.Set(key, value)
	}
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}
	
	// Set content type if not specified and body exists
	if req.Body != nil && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	
	// Log request
	client.Logger.Debug("API request",
		logger.String("method", req.Method),
		logger.String("url", url),
		logger.Any("headers", req.Headers))
	
	// Perform request
	resp, err := client.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	
	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	
	duration := time.Since(start)
	
	// Create test response
	testResp := &APITestResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       string(bodyBytes),
		Duration:   duration,
	}
	
	// Try to parse JSON response
	if isJSONContent(resp.Header.Get("Content-Type")) {
		var jsonBody map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &jsonBody); err == nil {
			testResp.JSON = jsonBody
		}
	}
	
	// Log response
	client.Logger.Debug("API response",
		logger.Int("status_code", resp.StatusCode),
		logger.Duration("duration", duration),
		logger.String("response_body", testResp.Body))
	
	return testResp, nil
}

// GET performs a GET request
func (client *APITestClient) GET(endpoint string) (*APITestResponse, error) {
	return client.Request(APITestRequest{
		Method:   "GET",
		Endpoint: endpoint,
	})
}

// POST performs a POST request
func (client *APITestClient) POST(endpoint string, body interface{}) (*APITestResponse, error) {
	return client.Request(APITestRequest{
		Method:   "POST",
		Endpoint: endpoint,
		Body:     body,
	})
}

// PUT performs a PUT request
func (client *APITestClient) PUT(endpoint string, body interface{}) (*APITestResponse, error) {
	return client.Request(APITestRequest{
		Method:   "PUT",
		Endpoint: endpoint,
		Body:     body,
	})
}

// DELETE performs a DELETE request
func (client *APITestClient) DELETE(endpoint string) (*APITestResponse, error) {
	return client.Request(APITestRequest{
		Method:   "DELETE",
		Endpoint: endpoint,
	})
}

// AssertStatusCode asserts the response status code
func (resp *APITestResponse) AssertStatusCode(expected int) error {
	if resp.StatusCode != expected {
		return fmt.Errorf("expected status code %d, got %d", expected, resp.StatusCode)
	}
	return nil
}

// AssertHeader asserts a response header value
func (resp *APITestResponse) AssertHeader(header, expected string) error {
	values, exists := resp.Headers[header]
	if !exists {
		return fmt.Errorf("header %s not found", header)
	}
	
	if len(values) == 0 || values[0] != expected {
		return fmt.Errorf("expected header %s to be %s, got %v", header, expected, values)
	}
	
	return nil
}

// AssertJSONField asserts a JSON field value
func (resp *APITestResponse) AssertJSONField(field string, expected interface{}) error {
	if resp.JSON == nil {
		return fmt.Errorf("response is not JSON")
	}
	
	value, exists := resp.JSON[field]
	if !exists {
		return fmt.Errorf("field %s not found in JSON response", field)
	}
	
	if value != expected {
		return fmt.Errorf("expected field %s to be %v, got %v", field, expected, value)
	}
	
	return nil
}

// AssertContains asserts that the response body contains a string
func (resp *APITestResponse) AssertContains(substring string) error {
	if !strings.Contains(resp.Body, substring) {
		return fmt.Errorf("response body does not contain %q", substring)
	}
	return nil
}

// AssertDuration asserts that the response time is within expected range
func (resp *APITestResponse) AssertDuration(maxDuration time.Duration) error {
	if resp.Duration > maxDuration {
		return fmt.Errorf("response took %v, expected less than %v", resp.Duration, maxDuration)
	}
	return nil
}

// RunAssertions runs multiple assertions on the response
func (resp *APITestResponse) RunAssertions(assertions []APITestAssertion) []error {
	var errors []error
	
	for _, assertion := range assertions {
		var err error
		
		switch assertion.Field {
		case "status_code":
			if expected, ok := assertion.Expected.(int); ok {
				err = resp.AssertStatusCode(expected)
			} else {
				err = fmt.Errorf("invalid type for status_code assertion")
			}
		case "duration":
			if expected, ok := assertion.Expected.(string); ok {
				if duration, parseErr := time.ParseDuration(expected); parseErr == nil {
					err = resp.AssertDuration(duration)
				} else {
					err = fmt.Errorf("invalid duration format: %s", expected)
				}
			}
		case "contains":
			if expected, ok := assertion.Expected.(string); ok {
				err = resp.AssertContains(expected)
			}
		default:
			// Assume it's a JSON field
			err = resp.AssertJSONField(assertion.Field, assertion.Expected)
		}
		
		if err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", assertion.Description, err))
		}
	}
	
	return errors
}

// LoadTestScenario represents a load testing scenario
type LoadTestScenario struct {
	Name              string                 `json:"name"`
	Requests          []APITestRequest       `json:"requests"`
	Concurrent        int                    `json:"concurrent"`
	Duration          time.Duration          `json:"duration"`
	RequestsPerSecond int                    `json:"requests_per_second"`
	Assertions        []APITestAssertion     `json:"assertions"`
}

// LoadTestResult represents load test results
type LoadTestResult struct {
	Scenario          string        `json:"scenario"`
	TotalRequests     int           `json:"total_requests"`
	SuccessfulRequests int          `json:"successful_requests"`
	FailedRequests     int          `json:"failed_requests"`
	AverageLatency     time.Duration `json:"average_latency"`
	MinLatency         time.Duration `json:"min_latency"`
	MaxLatency         time.Duration `json:"max_latency"`
	RequestsPerSecond  float64       `json:"requests_per_second"`
	Errors             []string      `json:"errors"`
}

// RunLoadTest executes a load test scenario
func (client *APITestClient) RunLoadTest(ctx context.Context, scenario LoadTestScenario) (*LoadTestResult, error) {
	client.Logger.Info("Starting load test", 
		logger.String("scenario", scenario.Name),
		logger.Int("concurrent", scenario.Concurrent),
		logger.Duration("duration", scenario.Duration))
	
	result := &LoadTestResult{
		Scenario:    scenario.Name,
		MinLatency:  time.Hour, // Will be updated with actual values
		Errors:      make([]string, 0),
	}
	
	start := time.Now()
	testCtx, cancel := context.WithTimeout(ctx, scenario.Duration)
	defer cancel()
	
	// Channel for collecting results
	results := make(chan *APITestResponse, scenario.Concurrent)
	errors := make(chan error, scenario.Concurrent)
	
	// Start concurrent workers
	for i := 0; i < scenario.Concurrent; i++ {
		go func() {
			for {
				select {
				case <-testCtx.Done():
					return
				default:
					// Execute requests from scenario
					for _, req := range scenario.Requests {
						resp, err := client.Request(req)
						if err != nil {
							select {
							case errors <- err:
							case <-testCtx.Done():
								return
							}
						} else {
							select {
							case results <- resp:
							case <-testCtx.Done():
								return
							}
						}
					}
				}
			}
		}()
	}
	
	// Collect results
	var totalLatency time.Duration
	
	for {
		select {
		case <-testCtx.Done():
			goto done
		case resp := <-results:
			result.TotalRequests++
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				result.SuccessfulRequests++
			} else {
				result.FailedRequests++
			}
			
			totalLatency += resp.Duration
			if resp.Duration < result.MinLatency {
				result.MinLatency = resp.Duration
			}
			if resp.Duration > result.MaxLatency {
				result.MaxLatency = resp.Duration
			}
		case err := <-errors:
			result.FailedRequests++
			result.TotalRequests++
			result.Errors = append(result.Errors, err.Error())
		}
	}
	
done:
	duration := time.Since(start)
	if result.TotalRequests > 0 {
		result.AverageLatency = totalLatency / time.Duration(result.TotalRequests)
		result.RequestsPerSecond = float64(result.TotalRequests) / duration.Seconds()
	}
	
	client.Logger.Info("Load test completed",
		logger.String("scenario", scenario.Name),
		logger.Int("total_requests", result.TotalRequests),
		logger.Int("successful", result.SuccessfulRequests),
		logger.Int("failed", result.FailedRequests),
		logger.Duration("avg_latency", result.AverageLatency),
		logger.Float64("rps", result.RequestsPerSecond))
	
	return result, nil
}

// Helper function to check if content type is JSON
func isJSONContent(contentType string) bool {
	return strings.Contains(contentType, "application/json") ||
		   strings.Contains(contentType, "application/problem+json")
}