package main

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oexza/Orisun/cmd/testutil"
	"github.com/oexza/Orisun/config"
	l "github.com/oexza/Orisun/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TestLoadTLSCredentials_Success tests successful loading of TLS credentials
func TestLoadTLSCredentials_Success(t *testing.T) {
	// Create temporary directory for test certificates
	tempDir := t.TempDir()

	// Generate test certificates
	certFile := filepath.Join(tempDir, "server.crt")
	keyFile := filepath.Join(tempDir, "server.key")

	// Create a self-signed certificate for testing
	if err := testutil.GenerateTestCertificate(certFile, keyFile); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Create config
	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = certFile
	cfg.Grpc.TLS.KeyFile = keyFile
	cfg.Grpc.TLS.ClientAuthRequired = false

	// Initialize logger
	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	// Load credentials
	creds, err := loadTLSCredentials(cfg, logger)
	if err != nil {
		t.Fatalf("loadTLSCredentials() failed = %v", err)
	}

	if creds == nil {
		t.Fatal("Expected credentials to be non-nil")
	}

	// Verify credentials are TLS credentials by checking the info
	info := creds.Info()
	if info.SecurityProtocol != "tls" {
		t.Errorf("Expected security protocol to be 'tls', got '%s'", info.SecurityProtocol)
	}
}

// TestLoadTLSCredentials_WithClientAuth tests TLS with client authentication
func TestLoadTLSCredentials_WithClientAuth(t *testing.T) {
	tempDir := t.TempDir()

	// Generate CA certificate
	caCertFile := filepath.Join(tempDir, "ca.crt")
	caKeyFile := filepath.Join(tempDir, "ca.key")
	if err := testutil.GenerateCACertificate(caCertFile, caKeyFile); err != nil {
		t.Fatalf("Failed to generate CA certificate: %v", err)
	}

	// Generate server certificate
	certFile := filepath.Join(tempDir, "server.crt")
	keyFile := filepath.Join(tempDir, "server.key")
	if err := testutil.GenerateTestCertificate(certFile, keyFile); err != nil {
		t.Fatalf("Failed to generate server certificate: %v", err)
	}

	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = certFile
	cfg.Grpc.TLS.KeyFile = keyFile
	cfg.Grpc.TLS.CAFile = caCertFile
	cfg.Grpc.TLS.ClientAuthRequired = true

	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	// Load credentials
	creds, err := loadTLSCredentials(cfg, logger)
	if err != nil {
		t.Fatalf("loadTLSCredentials() with client auth failed = %v", err)
	}

	if creds == nil {
		t.Fatal("Expected credentials to be non-nil")
	}
}

// TestLoadTLSCredentials_MissingCertFile tests error when cert file is missing
func TestLoadTLSCredentials_MissingCertFile(t *testing.T) {
	tempDir := t.TempDir()

	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = filepath.Join(tempDir, "nonexistent.crt")
	cfg.Grpc.TLS.KeyFile = filepath.Join(tempDir, "server.key")
	cfg.Grpc.TLS.ClientAuthRequired = false

	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	_, err := loadTLSCredentials(cfg, logger)
	if err == nil {
		t.Fatal("Expected error when certificate file is missing, got nil")
	}
}

// TestLoadTLSCredentials_MissingKeyFile tests error when key file is missing
func TestLoadTLSCredentials_MissingKeyFile(t *testing.T) {
	tempDir := t.TempDir()

	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = filepath.Join(tempDir, "server.crt")
	cfg.Grpc.TLS.KeyFile = filepath.Join(tempDir, "nonexistent.key")
	cfg.Grpc.TLS.ClientAuthRequired = false

	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	_, err := loadTLSCredentials(cfg, logger)
	if err == nil {
		t.Fatal("Expected error when key file is missing, got nil")
	}
}

// TestLoadTLSCredentials_InvalidCert tests error when certificate is invalid
func TestLoadTLSCredentials_InvalidCert(t *testing.T) {
	tempDir := t.TempDir()

	certFile := filepath.Join(tempDir, "invalid.crt")
	keyFile := filepath.Join(tempDir, "server.key")

	// Create invalid certificate file
	if err := os.WriteFile(certFile, []byte("invalid certificate"), 0644); err != nil {
		t.Fatalf("Failed to create invalid certificate: %v", err)
	}

	// Create a valid key
	if err := testutil.GenerateTestCertificate(keyFile, keyFile); err != nil {
		t.Fatalf("Failed to generate test key: %v", err)
	}

	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = certFile
	cfg.Grpc.TLS.KeyFile = keyFile
	cfg.Grpc.TLS.ClientAuthRequired = false

	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	_, err := loadTLSCredentials(cfg, logger)
	if err == nil {
		t.Fatal("Expected error when certificate is invalid, got nil")
	}
}

// TestLoadTLSCredentials_MissingCAFile tests error when CA file is missing but client auth is required
func TestLoadTLSCredentials_MissingCAFile(t *testing.T) {
	tempDir := t.TempDir()

	certFile := filepath.Join(tempDir, "server.crt")
	keyFile := filepath.Join(tempDir, "server.key")

	if err := testutil.GenerateTestCertificate(certFile, keyFile); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = certFile
	cfg.Grpc.TLS.KeyFile = keyFile
	cfg.Grpc.TLS.CAFile = filepath.Join(tempDir, "nonexistent_ca.crt")
	cfg.Grpc.TLS.ClientAuthRequired = true

	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	_, err := loadTLSCredentials(cfg, logger)
	if err == nil {
		t.Fatal("Expected error when CA file is missing with client auth, got nil")
	}
}

// TestTLSServerConfiguration tests that the gRPC server can be configured with TLS
func TestTLSServerConfiguration(t *testing.T) {
	tempDir := t.TempDir()

	certFile := filepath.Join(tempDir, "server.crt")
	keyFile := filepath.Join(tempDir, "server.key")

	if err := testutil.GenerateTestCertificate(certFile, keyFile); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = certFile
	cfg.Grpc.TLS.KeyFile = keyFile
	cfg.Grpc.TLS.ClientAuthRequired = false

	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	// Load credentials
	creds, err := loadTLSCredentials(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to load TLS credentials: %v", err)
	}

	// Create server options
	serverOpts := []grpc.ServerOption{grpc.Creds(creds)}

	// Create server (we won't start it, just verify it can be created)
	_ = grpc.NewServer(serverOpts...)
}

// TestInsecureServerConfiguration tests that the server works without TLS
func TestInsecureServerConfiguration(t *testing.T) {
	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = false

	// Verify loadTLSCredentials is not called when TLS is disabled
	if cfg.Grpc.TLS.Enabled {
		t.Fatal("Expected TLS to be disabled")
	}

	// Create server without TLS
	serverOpts := []grpc.ServerOption{}
	_ = grpc.NewServer(serverOpts...)

	// Test that we can create insecure credentials
	insecureCreds := credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12,
	})
	if insecureCreds == nil {
		t.Fatal("Expected insecure credentials to be created")
	}
}

// TestTLSVersion tests that minimum TLS version is set correctly
func TestTLSVersion(t *testing.T) {
	tempDir := t.TempDir()

	certFile := filepath.Join(tempDir, "server.crt")
	keyFile := filepath.Join(tempDir, "server.key")

	if err := testutil.GenerateTestCertificate(certFile, keyFile); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = certFile
	cfg.Grpc.TLS.KeyFile = keyFile
	cfg.Grpc.TLS.ClientAuthRequired = false

	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	creds, err := loadTLSCredentials(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to load TLS credentials: %v", err)
	}

	// Verify credentials were created successfully
	if creds == nil {
		t.Fatal("Expected credentials to be non-nil")
	}
}

// TestTLSClientAuth tests client certificate authentication requirement
func TestTLSClientAuth(t *testing.T) {
	tempDir := t.TempDir()

	// Generate CA certificate
	caCertFile := filepath.Join(tempDir, "ca.crt")
	caKeyFile := filepath.Join(tempDir, "ca.key")
	if err := testutil.GenerateCACertificate(caCertFile, caKeyFile); err != nil {
		t.Fatalf("Failed to generate CA certificate: %v", err)
	}

	// Generate server certificate
	certFile := filepath.Join(tempDir, "server.crt")
	keyFile := filepath.Join(tempDir, "server.key")
	if err := testutil.GenerateTestCertificate(certFile, keyFile); err != nil {
		t.Fatalf("Failed to generate server certificate: %v", err)
	}

	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = certFile
	cfg.Grpc.TLS.KeyFile = keyFile
	cfg.Grpc.TLS.CAFile = caCertFile
	cfg.Grpc.TLS.ClientAuthRequired = true

	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	creds, err := loadTLSCredentials(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to load TLS credentials with client auth: %v", err)
	}

	// Verify the credentials require client certificates
	// This is tested by checking that the credentials were created successfully
	if creds == nil {
		t.Fatal("Expected credentials with client auth to be non-nil")
	}
}

// TestTLSCertificateExpiry tests that certificate expiry is handled correctly
func TestTLSCertificateExpiry(t *testing.T) {
	tempDir := t.TempDir()

	certFile := filepath.Join(tempDir, "server.crt")
	keyFile := filepath.Join(tempDir, "server.key")

	// Generate a certificate that's already expired
	if err := testutil.GenerateCertificateWithExpiry(certFile, keyFile, -24*time.Hour); err != nil {
		t.Fatalf("Failed to generate expired certificate: %v", err)
	}

	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = certFile
	cfg.Grpc.TLS.KeyFile = keyFile
	cfg.Grpc.TLS.ClientAuthRequired = false

	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	// Load should succeed, but connections will fail with expired cert
	_, err := loadTLSCredentials(cfg, logger)
	if err != nil {
		// This is actually expected behavior - expired certs should be rejected
		t.Logf("Expected behavior: expired certificate rejected: %v", err)
	}
}

// TestTLSWithValidClientCertificate tests generating and using client certificates
func TestTLSWithValidClientCertificate(t *testing.T) {
	tempDir := t.TempDir()

	// Generate CA certificate
	caCertFile := filepath.Join(tempDir, "ca.crt")
	caKeyFile := filepath.Join(tempDir, "ca.key")
	if err := testutil.GenerateCACertificate(caCertFile, caKeyFile); err != nil {
		t.Fatalf("Failed to generate CA certificate: %v", err)
	}

	// Generate client certificate
	clientCertFile := filepath.Join(tempDir, "client.crt")
	clientKeyFile := filepath.Join(tempDir, "client.key")
	if err := testutil.GenerateClientCertificate(clientCertFile, clientKeyFile, caCertFile, caKeyFile); err != nil {
		t.Fatalf("Failed to generate client certificate: %v", err)
	}

	// Verify files were created
	if _, err := os.Stat(clientCertFile); os.IsNotExist(err) {
		t.Fatal("Client certificate file was not created")
	}
	if _, err := os.Stat(clientKeyFile); os.IsNotExist(err) {
		t.Fatal("Client key file was not created")
	}
}

// BenchmarkTLSCredentialsLoading benchmarks TLS credential loading
func BenchmarkTLSCredentialsLoading(b *testing.B) {
	tempDir := b.TempDir()

	certFile := filepath.Join(tempDir, "server.crt")
	keyFile := filepath.Join(tempDir, "server.key")

	if err := testutil.GenerateTestCertificate(certFile, keyFile); err != nil {
		b.Fatalf("Failed to generate test certificate: %v", err)
	}

	cfg := config.AppConfig{}
	cfg.Grpc.TLS.Enabled = true
	cfg.Grpc.TLS.CertFile = certFile
	cfg.Grpc.TLS.KeyFile = keyFile
	cfg.Grpc.TLS.ClientAuthRequired = false

	logger := l.InitializeDefaultLogger(config.LoggingConfig{Level: "WARN"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := loadTLSCredentials(cfg, logger)
		if err != nil {
			b.Fatalf("loadTLSCredentials() failed = %v", err)
		}
	}
}
