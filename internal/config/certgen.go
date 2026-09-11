package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// caIdentity bundles a parsed CA certificate and its private key.
type caIdentity struct {
	cert *x509.Certificate
	key  *rsa.PrivateKey
}

// createCA generates a fresh self-signed root CA.
func createCA(cn string, validDays int, now time.Time) (capem, keypem []byte, ca *caIdentity, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(now.Unix()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(validDays, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, err
	}
	capem = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keypem = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return capem, keypem, &caIdentity{cert: parsed, key: key}, nil
}

// createSignedCert issues a certificate signed by the given CA.
func createSignedCert(ca *caIdentity, cn string, org []string, usage x509.ExtKeyUsage, validDays int, now time.Time, ips []net.IP, dns []string) (certpem, keypem []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		Subject:      pkix.Name{CommonName: cn, Organization: org},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(validDays, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  ips,
		DNSNames:     dns,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		return nil, nil, fmt.Errorf("sign %s: %w", cn, err)
	}
	certpem = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keypem = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certpem, keypem, nil
}

// parseCAPEM loads a CA certificate (and optional key) from PEM bytes.
func parseCAPEM(capem []byte) (*caIdentity, error) {
	block, _ := pem.Decode(capem)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("no CERTIFICATE PEM block found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return &caIdentity{cert: cert}, nil
}
