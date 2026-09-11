package apid

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc/credentials"

	"github.com/HuntedRaven7/Archaeopteryx/internal/config"
)

// reloadableCreds re-reads the node PKI from disk on every handshake, so
// ApplyConfig can swap the trust anchor and server identity without a daemon
// restart. In-flight connections are unaffected; new ones see the new CA.
type reloadableCreds struct {
	dir string
}

// newReloadableCreds returns transport credentials backed by stateDir.
func newReloadableCreds(stateDir string) credentials.TransportCredentials {
	return &reloadableCreds{dir: stateDir}
}

func (r *reloadableCreds) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	cfg, err := clientTLSConfig(r.dir)
	if err != nil {
		return nil, nil, err
	}
	return credentials.NewTLS(cfg).ClientHandshake(ctx, authority, rawConn)
}

func (r *reloadableCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	cfg, err := serverTLSConfig(r.dir)
	if err != nil {
		return nil, nil, err
	}
	return credentials.NewTLS(cfg).ServerHandshake(rawConn)
}

func (r *reloadableCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{
		SecurityProtocol: "tls",
		SecurityVersion:  "1.3",
		ServerName:       "",
	}
}

func (r *reloadableCreds) Clone() credentials.TransportCredentials {
	return &reloadableCreds{dir: r.dir}
}

func (r *reloadableCreds) OverrideServerName(serverName string) error {
	return nil
}

// serverTLSConfig builds the server mTLS config from the current on-disk CA,
// server identity. The client cert requirement and CA pool follow the node
// trust anchor (bootstrap CA until a machine config is applied).
func serverTLSConfig(dir string) (*tls.Config, error) {
	serverCert, err := tls.LoadX509KeyPair(
		filepath.Join(dir, config.ServerFile),
		filepath.Join(dir, config.ServerKey),
	)
	if err != nil {
		return nil, fmt.Errorf("load server identity: %w", err)
	}
	capem, err := os.ReadFile(filepath.Join(dir, config.CAFile))
	if err != nil {
		return nil, fmt.Errorf("read ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(capem) {
		return nil, fmt.Errorf("parse CA certificate")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

func clientTLSConfig(dir string) (*tls.Config, error) {
	clientCert, err := tls.LoadX509KeyPair(
		filepath.Join(dir, config.AdminFile),
		filepath.Join(dir, config.AdminKey),
	)
	if err != nil {
		return nil, fmt.Errorf("load admin identity: %w", err)
	}
	capem, err := os.ReadFile(filepath.Join(dir, config.CAFile))
	if err != nil {
		return nil, fmt.Errorf("read ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(capem) {
		return nil, fmt.Errorf("parse CA certificate")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}
