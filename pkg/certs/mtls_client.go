// Package certs provides mTLS client functionality
package certs

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// MTLSClient provides HTTP client with mutual TLS authentication
type MTLSClient struct {
	client    *http.Client
	logger    logger.Logger
	serviceName string
}

// MTLSConfig holds configuration for mTLS client
type MTLSConfig struct {
	ServiceName     string
	ClientCertFile  string
	ClientKeyFile   string
	CACertFile      string
	ServerName      string
	Timeout         time.Duration
	InsecureSkipVerify bool // For development only
}

// NewMTLSClient creates a new mTLS-enabled HTTP client
func NewMTLSClient(config MTLSConfig, log logger.Logger) (*MTLSClient, error) {
	// Load client certificate
	clientCert, err := tls.LoadX509KeyPair(config.ClientCertFile, config.ClientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Load CA certificate
	caCert, err := os.ReadFile(config.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{clientCert},
		RootCAs:           caCertPool,
		ServerName:        config.ServerName,
		InsecureSkipVerify: config.InsecureSkipVerify,
		MinVersion:        tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		},
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Create HTTP client
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:       tlsConfig,
			TLSHandshakeTimeout:   10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	log.Info("Created mTLS client",
		logger.String("service", config.ServiceName),
		logger.String("server_name", config.ServerName))

	return &MTLSClient{
		client:      client,
		logger:      log,
		serviceName: config.ServiceName,
	}, nil
}

// GetClient returns the underlying HTTP client
func (m *MTLSClient) GetClient() *http.Client {
	return m.client
}

// Do executes an HTTP request using mTLS
func (m *MTLSClient) Do(req *http.Request) (*http.Response, error) {
	// Add service identification header
	req.Header.Set("X-Service-Name", m.serviceName)
	req.Header.Set("User-Agent", fmt.Sprintf("scholarplatforms-%s-service", m.serviceName))

	start := time.Now()
	resp, err := m.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		m.logger.Error("mTLS request failed",
			logger.String("method", req.Method),
			logger.String("url", req.URL.String()),
			logger.Duration("duration", duration),
			logger.ErrorField(err))
		return nil, err
	}

	m.logger.Debug("mTLS request completed",
		logger.String("method", req.Method),
		logger.String("url", req.URL.String()),
		logger.Int("status", resp.StatusCode),
		logger.Duration("duration", duration))

	return resp, nil
}

// CreateTLSConfig creates a TLS configuration for servers
func CreateTLSConfig(certFile, keyFile, caCertFile string, requireClientCert bool) (*tls.Config, error) {
	// Load server certificate
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		},
		PreferServerCipherSuites: true,
	}

	if requireClientCert && caCertFile != "" {
		// Load CA certificate for client verification
		caCert, err := os.ReadFile(caCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = caCertPool
	}

	return tlsConfig, nil
}

// ValidateClientCertificate validates a client certificate against allowed service names
func ValidateClientCertificate(cert *x509.Certificate, allowedServices []string) error {
	if cert == nil {
		return fmt.Errorf("no client certificate provided")
	}

	// Extract service name from certificate subject
	serviceName := cert.Subject.CommonName

	// Check if service is allowed
	for _, allowed := range allowedServices {
		if serviceName == allowed {
			return nil
		}
	}

	return fmt.Errorf("service '%s' not authorized", serviceName)
}

// ExtractServiceNameFromCert extracts the service name from a client certificate
func ExtractServiceNameFromCert(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	return cert.Subject.CommonName
}