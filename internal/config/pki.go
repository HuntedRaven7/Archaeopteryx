// Package config holds daemon configuration and the apxconfig machine
// configuration schema plus the TLS identity tooling used to bootstrap mTLS.
package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// TLS files managed by apxd under its state directory.
const (
	CAFile       = "ca.crt"
	CAKeyFile    = "ca.key"
	ServerFile   = "server.crt"
	ServerKey    = "server.key"
	AdminFile    = "admin.crt"
	AdminKey     = "admin.key"
	onboarding   = "onboarding-token"
	caTTLDays    = 3650 // 10 years
	adminTTLDays = 3650
)

// PKI bundles the on-disk identities for a node.
type PKI struct {
	dir string

	capem  []byte
	keypem []byte
}

// LoadPKI loads an existing node PKI from dir.
func LoadPKI(dir string) (*PKI, error) {
	p := &PKI{dir: dir}
	var err error
	if p.capem, err = os.ReadFile(filepath.Join(dir, CAFile)); err != nil {
		return nil, err
	}
	if p.keypem, err = os.ReadFile(filepath.Join(dir, CAKeyFile)); err != nil {
		return nil, err
	}
	return p, nil
}

// Derive or load the node PKI, generating all identities on first boot.
func EnsurePKI(dir string) (*PKI, error) {
	if _, err := os.Stat(filepath.Join(dir, CAFile)); err == nil {
		return LoadPKI(dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return generatePKI(dir)
}

func generatePKI(dir string) (*PKI, error) {
	now := time.Now()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(now.Unix()),
		Subject:               pkix.Name{CommonName: "apx-ca"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(caTTLDays, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDer, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	capem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDer})
	keypem := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})
	if err := writePriv(filepath.Join(dir, CAFile), capem); err != nil {
		return nil, err
	}
	if err := writePriv(filepath.Join(dir, CAKeyFile), keypem); err != nil {
		return nil, err
	}
	p := &PKI{dir: dir, capem: capem, keypem: keypem}

	hostname, _ := os.Hostname()
	ips := localIPs()
	if _, err := p.issue("apxd", nil, x509.ExtKeyUsageServerAuth, now, ips,
		[]string{"localhost", hostname, "kubernetes"}); err != nil {
		return nil, err
	}
	if _, err := p.issue("admin", []string{"system:masters"}, x509.ExtKeyUsageClientAuth, now, nil, nil); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *PKI) issue(cn string, org []string, usage x509.ExtKeyUsage, now time.Time, ips []net.IP, dns []string) (tlsIdentity, error) {
	caDer, _ := pem.Decode(p.capem)
	keyDer, _ := pem.Decode(p.keypem)
	if caDer == nil || keyDer == nil {
		return tlsIdentity{}, errors.New("corrupt CA PEM")
	}
	ca, err := x509.ParseCertificate(caDer.Bytes)
	if err != nil {
		return tlsIdentity{}, err
	}
	caKey, err := x509.ParsePKCS1PrivateKey(keyDer.Bytes)
	if err != nil {
		return tlsIdentity{}, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tlsIdentity{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		Subject:      pkix.Name{CommonName: cn, Organization: org},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(adminTTLDays, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  ips,
		DNSNames:     dns,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return tlsIdentity{}, err
	}
	certFile, keyFile := ServerFile, ServerKey
	if usage == x509.ExtKeyUsageClientAuth {
		certFile, keyFile = AdminFile, AdminKey
	}
	cpem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kpem := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := writePriv(filepath.Join(p.dir, certFile), cpem); err != nil {
		return tlsIdentity{}, err
	}
	if err := writePriv(filepath.Join(p.dir, keyFile), kpem); err != nil {
		return tlsIdentity{}, err
	}
	return tlsIdentity{cert: cpem, key: kpem}, nil
}

type tlsIdentity struct {
	cert []byte
	key  []byte
}

// Path returns the CA certificate path.
func (p *PKI) CAPath() string { return filepath.Join(p.dir, CAFile) }

// Dir returns the PKI state directory.
func (p *PKI) Dir() string { return p.dir }

// ReadAdminPath returns paths for the admin client identity.
func (p *PKI) ReadAdminPath() (ca, cert, key string) {
	return p.CAPath(), filepath.Join(p.dir, AdminFile), filepath.Join(p.dir, AdminKey)
}

// CAPEM returns the CA certificate in PEM form.
func (p *PKI) CAPEM() []byte { return p.capem }

func localIPs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	if err != nil {
		return ips
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
			ips = append(ips, ipn.IP)
		}
	}
	return ips
}

func writePriv(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// DefaultListen is the default gRPC listen address (Talos machine API parity).
const DefaultListen = "0.0.0.0:50000"

// DefaultStateDir is where apxd keeps its PKI and runtime state.
const DefaultStateDir = "/var/lib/archaeopteryx"

// LoadOrCreateOnboardingToken returns the maintenance-mode onboarding token for
// this boot, creating it on first start. Used only while the node is
// unconfigured; later milestones gate it behind apxconfig.
func LoadOrCreateOnboardingToken(dir string) (string, error) {
	path := filepath.Join(dir, onboarding)
	if b, err := os.ReadFile(path); err == nil {
		return string(b), nil
	}
	tok, err := randomToken(32)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(tok), 0o600); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

func randomToken(n int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b), nil
}
