// Package discovery provides service discovery functionality for microservices
package discovery

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// ServiceRegistry holds service endpoint information
type ServiceRegistry struct {
	services map[string]*ServiceEndpoint
	logger   logger.Logger
}

// ServiceEndpoint represents a service endpoint with health checking
type ServiceEndpoint struct {
	Name            string        `json:"name"`
	URL             string        `json:"url"`
	HealthPath      string        `json:"health_path"`
	Timeout         time.Duration `json:"timeout"`
	IsHealthy       bool          `json:"is_healthy"`
	LastHealthCheck time.Time     `json:"last_health_check"`
}

// ServiceConfig holds configuration for a service endpoint
type ServiceConfig struct {
	Name       string
	LocalURL   string // URL for local development
	DockerURL  string // URL for Docker environment
	HealthPath string
	Timeout    time.Duration
}

// NewServiceRegistry creates a new service registry
func NewServiceRegistry(log logger.Logger) *ServiceRegistry {
	return &ServiceRegistry{
		services: make(map[string]*ServiceEndpoint),
		logger:   log,
	}
}

// RegisterService registers a service with the registry
func (r *ServiceRegistry) RegisterService(config ServiceConfig) {
	url := config.LocalURL
	
	// Check if running in Docker environment
	if IsDockerEnvironment() {
		url = config.DockerURL
		r.logger.Info("Using Docker service URL", 
			logger.String("service", config.Name),
			logger.String("url", url))
	} else {
		r.logger.Info("Using local service URL", 
			logger.String("service", config.Name),
			logger.String("url", url))
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	healthPath := config.HealthPath
	if healthPath == "" {
		healthPath = "/health"
	}

	r.services[config.Name] = &ServiceEndpoint{
		Name:       config.Name,
		URL:        url,
		HealthPath: healthPath,
		Timeout:    timeout,
		IsHealthy:  false,
	}
}

// GetServiceURL returns the URL for a registered service
func (r *ServiceRegistry) GetServiceURL(serviceName string) (string, error) {
	service, exists := r.services[serviceName]
	if !exists {
		return "", fmt.Errorf("service '%s' not registered", serviceName)
	}
	return service.URL, nil
}

// GetServiceEndpoint returns the full service endpoint info
func (r *ServiceRegistry) GetServiceEndpoint(serviceName string) (*ServiceEndpoint, error) {
	service, exists := r.services[serviceName]
	if !exists {
		return nil, fmt.Errorf("service '%s' not registered", serviceName)
	}
	return service, nil
}

// HealthCheck performs health check on a specific service
func (r *ServiceRegistry) HealthCheck(ctx context.Context, serviceName string) error {
	service, exists := r.services[serviceName]
	if !exists {
		return fmt.Errorf("service '%s' not registered", serviceName)
	}

	client := &http.Client{Timeout: service.Timeout}
	healthURL := service.URL + service.HealthPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		service.IsHealthy = false
		service.LastHealthCheck = time.Now()
		r.logger.Error("Health check failed", 
			logger.String("service", serviceName),
			logger.String("url", healthURL),
			logger.ErrorField(err))
		return fmt.Errorf("health check failed for service '%s': %w", serviceName, err)
	}
	defer resp.Body.Close()

	service.LastHealthCheck = time.Now()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		service.IsHealthy = true
		r.logger.Debug("Health check passed", 
			logger.String("service", serviceName),
			logger.Int("status", resp.StatusCode))
		return nil
	}

	service.IsHealthy = false
	r.logger.Warn("Health check failed", 
		logger.String("service", serviceName),
		logger.Int("status", resp.StatusCode))
	return fmt.Errorf("health check failed for service '%s': status %d", serviceName, resp.StatusCode)
}

// HealthCheckAll performs health checks on all registered services
func (r *ServiceRegistry) HealthCheckAll(ctx context.Context) map[string]error {
	results := make(map[string]error)
	
	for serviceName := range r.services {
		err := r.HealthCheck(ctx, serviceName)
		results[serviceName] = err
	}
	
	return results
}

// ListServices returns all registered services
func (r *ServiceRegistry) ListServices() map[string]*ServiceEndpoint {
	services := make(map[string]*ServiceEndpoint)
	for name, endpoint := range r.services {
		// Create a copy to avoid external modification
		services[name] = &ServiceEndpoint{
			Name:            endpoint.Name,
			URL:             endpoint.URL,
			HealthPath:      endpoint.HealthPath,
			Timeout:         endpoint.Timeout,
			IsHealthy:       endpoint.IsHealthy,
			LastHealthCheck: endpoint.LastHealthCheck,
		}
	}
	return services
}

// IsDockerEnvironment checks if the application is running in a Docker container
func IsDockerEnvironment() bool {
	// Check multiple indicators of Docker environment
	
	// 1. Check for /.dockerenv file (most reliable)
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	
	// 2. Check if running as PID 1 (common in containers)
	if os.Getpid() == 1 {
		return true
	}
	
	// 3. Check for Docker-specific environment variables
	if os.Getenv("DOCKER_CONTAINER") == "true" {
		return true
	}
	
	// 4. Check /proc/1/cgroup for Docker indicators (Linux only)
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") || strings.Contains(content, "containerd") {
			return true
		}
	}
	
	// 5. Check hostname patterns (less reliable but useful)
	if hostname, err := os.Hostname(); err == nil {
		// Docker containers often have hex-like hostnames
		if len(hostname) == 12 && isHexString(hostname) {
			return true
		}
		// Check for compose service names
		if strings.Contains(hostname, "scholarplatforms-") {
			return true
		}
	}
	
	return false
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// GetEnvironmentType returns the detected environment type
func GetEnvironmentType() string {
	if IsDockerEnvironment() {
		return "docker"
	}
	return "local"
}