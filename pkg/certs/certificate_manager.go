// Package certs provides certificate management for mutual TLS authentication
package certs

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// CertificateManager handles certificate generation and management
type CertificateManager struct {
	certDir    string
	caCert     *x509.Certificate
	caKey      *rsa.PrivateKey
	logger     logger.Logger
	commonName string
}

// ServiceCertificate holds certificate and key data for a service
type ServiceCertificate struct {
	ServiceName string
	CertPEM     []byte
	KeyPEM      []byte
	CertFile    string
	KeyFile     string
	TLSCert     tls.Certificate
}

// CertificateConfig holds configuration for certificate generation
type CertificateConfig struct {
	ServiceName    string
	CommonName     string
	Organization   string
	Country        string
	Province       string
	Locality       string
	DNSNames       []string
	IPAddresses    []net.IP
	ValidityPeriod time.Duration
}

// NewCertificateManager creates a new certificate manager
func NewCertificateManager(certDir string, commonName string, log logger.Logger) (*CertificateManager, error) {
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create certificate directory: %w", err)
	}

	manager := &CertificateManager{
		certDir:    certDir,
		logger:     log,
		commonName: commonName,
	}

	// Load or create CA certificate
	if err := manager.loadOrCreateCA(); err != nil {
		return nil, fmt.Errorf("failed to initialize CA: %w", err)
	}

	return manager, nil
}

// loadOrCreateCA loads existing CA certificate or creates a new one
func (cm *CertificateManager) loadOrCreateCA() error {
	caCertFile := filepath.Join(cm.certDir, "ca-cert.pem")
	caKeyFile := filepath.Join(cm.certDir, "ca-key.pem")

	// Check if CA files exist
	if _, err := os.Stat(caCertFile); err == nil {
		if _, err := os.Stat(caKeyFile); err == nil {
			// Load existing CA
			return cm.loadCA(caCertFile, caKeyFile)
		}
	}

	// Create new CA
	cm.logger.Info("Creating new CA certificate")
	return cm.createCA(caCertFile, caKeyFile)
}

// loadCA loads existing CA certificate and key
func (cm *CertificateManager) loadCA(certFile, keyFile string) error {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return fmt.Errorf("failed to read CA certificate: %w", err)
	}

	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return fmt.Errorf("failed to read CA key: %w", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return fmt.Errorf("failed to decode CA certificate PEM")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to decode CA key PEM")
	}

	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA key: %w", err)
	}

	cm.caCert = cert
	cm.caKey = key

	cm.logger.Info("Loaded existing CA certificate",
		logger.String("subject", cert.Subject.CommonName),
		logger.Time("expires", cert.NotAfter))

	return nil
}

// createCA creates a new CA certificate and key
func (cm *CertificateManager) createCA(certFile, keyFile string) error {
	// Generate CA private key
	caKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("failed to generate CA key: %w", err)
	}

	// Create CA certificate template
	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:         cm.commonName + " CA",
			Organization:       []string{"ScholarPlatforms"},
			Country:            []string{"US"},
			Province:           []string{""},
			Locality:           []string{""},
			OrganizationalUnit: []string{"IT"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	// Create certificate
	caCertBytes, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("failed to create CA certificate: %w", err)
	}

	// Save certificate
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertBytes,
	})

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		return fmt.Errorf("failed to write CA certificate: %w", err)
	}

	// Save private key
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caKey),
	})

	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write CA key: %w", err)
	}

	// Parse the created certificate
	caCert, err := x509.ParseCertificate(caCertBytes)
	if err != nil {
		return fmt.Errorf("failed to parse created CA certificate: %w", err)
	}

	cm.caCert = caCert
	cm.caKey = caKey

	cm.logger.Info("Created new CA certificate",
		logger.String("subject", caCert.Subject.CommonName),
		logger.Time("expires", caCert.NotAfter))

	return nil
}

// GenerateServiceCertificate generates a certificate for a specific service
func (cm *CertificateManager) GenerateServiceCertificate(config CertificateConfig) (*ServiceCertificate, error) {
	// Set defaults
	if config.Organization == "" {
		config.Organization = "ScholarPlatforms"
	}
	if config.Country == "" {
		config.Country = "US"
	}
	if config.ValidityPeriod == 0 {
		config.ValidityPeriod = 365 * 24 * time.Hour // 1 year
	}
	if config.CommonName == "" {
		config.CommonName = config.ServiceName
	}

	// Generate service private key
	serviceKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate service key: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:         config.CommonName,
			Organization:       []string{config.Organization},
			Country:            []string{config.Country},
			Province:           []string{config.Province},
			Locality:           []string{config.Locality},
			OrganizationalUnit: []string{"Services"},
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(config.ValidityPeriod),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     config.DNSNames,
		IPAddresses:  config.IPAddresses,
	}

	// Create certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, cm.caCert, &serviceKey.PublicKey, cm.caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create service certificate: %w", err)
	}

	// Encode certificate
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	// Encode private key
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(serviceKey),
	})

	// Save to files
	certFile := filepath.Join(cm.certDir, config.ServiceName+"-cert.pem")
	keyFile := filepath.Join(cm.certDir, config.ServiceName+"-key.pem")

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		return nil, fmt.Errorf("failed to write service certificate: %w", err)
	}

	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		return nil, fmt.Errorf("failed to write service key: %w", err)
	}

	// Create TLS certificate
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS certificate: %w", err)
	}

	serviceCert := &ServiceCertificate{
		ServiceName: config.ServiceName,
		CertPEM:     certPEM,
		KeyPEM:      keyPEM,
		CertFile:    certFile,
		KeyFile:     keyFile,
		TLSCert:     tlsCert,
	}

	cm.logger.Info("Generated service certificate",
		logger.String("service", config.ServiceName),
		logger.String("common_name", config.CommonName),
		logger.Time("expires", template.NotAfter))

	return serviceCert, nil
}

// GetCACertificatePEM returns the CA certificate in PEM format
func (cm *CertificateManager) GetCACertificatePEM() ([]byte, error) {
	if cm.caCert == nil {
		return nil, fmt.Errorf("CA certificate not loaded")
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cm.caCert.Raw,
	}), nil
}

// GetCACertificateFile returns the path to the CA certificate file
func (cm *CertificateManager) GetCACertificateFile() string {
	return filepath.Join(cm.certDir, "ca-cert.pem")
}

// LoadServiceCertificate loads an existing service certificate
func (cm *CertificateManager) LoadServiceCertificate(serviceName string) (*ServiceCertificate, error) {
	certFile := filepath.Join(cm.certDir, serviceName+"-cert.pem")
	keyFile := filepath.Join(cm.certDir, serviceName+"-key.pem")

	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read service certificate: %w", err)
	}

	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read service key: %w", err)
	}

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	return &ServiceCertificate{
		ServiceName: serviceName,
		CertPEM:     certPEM,
		KeyPEM:      keyPEM,
		CertFile:    certFile,
		KeyFile:     keyFile,
		TLSCert:     tlsCert,
	}, nil
}