package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	caCertFile = "proxy-ca.pem"
	caKeyFile  = "proxy-ca-key.pem"
)

// EnsureCA loads or generates a self-signed CA certificate for the MITM proxy.
// The cert and key are stored in coralDir (~/.coral/).
func EnsureCA(coralDir string) (tls.Certificate, error) {
	certPath := filepath.Join(coralDir, caCertFile)
	keyPath := filepath.Join(coralDir, caKeyFile)

	// Try loading existing CA
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		// Verify it hasn't expired
		leaf, parseErr := x509.ParseCertificate(cert.Certificate[0])
		if parseErr == nil && time.Now().Before(leaf.NotAfter) {
			slog.Info("[proxy] loaded existing CA certificate", "path", certPath, "expires", leaf.NotAfter.Format("2006-01-02"))
			cert.Leaf = leaf
			return cert, nil
		}
		slog.Warn("[proxy] existing CA certificate expired or invalid, regenerating")
	}

	// Generate new CA
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate CA key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Coral MITM Proxy"},
			CommonName:   "Coral Proxy CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create CA certificate: %w", err)
	}

	// Write cert PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return tls.Certificate{}, fmt.Errorf("write CA cert: %w", err)
	}

	// Write key PEM
	keyDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal CA key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return tls.Certificate{}, fmt.Errorf("write CA key: %w", err)
	}

	slog.Info("[proxy] generated new CA certificate", "path", certPath)

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse generated CA: %w", err)
	}
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	cert.Leaf = leaf
	return cert, nil
}

// CACertPath returns the path to the CA certificate PEM file.
func CACertPath(coralDir string) string {
	return filepath.Join(coralDir, caCertFile)
}

// EnsureCABundle creates a combined PEM bundle with system CA certs + the Coral
// MITM CA cert. This is used as SSL_CERT_FILE for agents so their HTTP clients
// trust both real upstream certs and our MITM-generated certs.
func EnsureCABundle(coralDir string) error {
	bundlePath := filepath.Join(coralDir, "proxy-ca-bundle.pem")
	caPath := filepath.Join(coralDir, caCertFile)

	coralCA, err := os.ReadFile(caPath)
	if err != nil {
		return fmt.Errorf("read Coral CA: %w", err)
	}

	// Load system CA certs
	systemPool, err := x509.SystemCertPool()
	if err != nil {
		slog.Warn("[proxy] could not load system cert pool, bundle will only contain Coral CA", "error", err)
		return os.WriteFile(bundlePath, coralCA, 0644)
	}

	// Export system certs as PEM
	var bundle []byte
	for _, cert := range systemPool.Subjects() { //nolint: staticcheck
		_ = cert // Subjects() is deprecated but we need the raw PEM approach below
	}

	// On macOS/Linux, read from common system cert bundle paths
	systemPaths := []string{
		"/etc/ssl/certs/ca-certificates.crt", // Debian/Ubuntu
		"/etc/pki/tls/certs/ca-bundle.crt",   // RHEL/Fedora
		"/etc/ssl/cert.pem",                   // macOS/Alpine
	}
	for _, p := range systemPaths {
		if data, err := os.ReadFile(p); err == nil {
			bundle = data
			break
		}
	}

	// Append our CA cert
	bundle = append(bundle, '\n')
	bundle = append(bundle, coralCA...)

	if err := os.WriteFile(bundlePath, bundle, 0644); err != nil {
		return fmt.Errorf("write CA bundle: %w", err)
	}
	slog.Info("[proxy] created CA bundle", "path", bundlePath, "size", len(bundle))
	return nil
}

// generateHostCert creates a TLS certificate for the given hostname,
// signed by the CA certificate. Certs are short-lived (24h).
func generateHostCert(host string, ca tls.Certificate) (*tls.Certificate, error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate host key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: host,
		},
		DNSNames:  []string{host},
		NotBefore: time.Now().Add(-5 * time.Minute),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.Leaf, &privKey.PublicKey, ca.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("create host certificate: %w", err)
	}

	hostCert := &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  privKey,
	}
	return hostCert, nil
}
