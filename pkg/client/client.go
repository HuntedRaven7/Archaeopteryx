// Package client provides the gRPC client used by apxctl to talk to apxd.
package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
)

// Options configure how apxctl connects to a node.
type Options struct {
	Endpoint string // host:port of apxd
	CA       string // path to CA PEM
	Cert     string // path to client cert PEM
	Key      string // path to client key PEM
	Insecure bool   // skip TLS verification and client cert
}

// Client wraps a gRPC connection plus the typed API clients.
type Client struct {
	conn    *grpc.ClientConn
	Machine apxv1.MachineServiceClient
}

// New dials the given endpoint with the configured mTLS material.
func New(opts Options) (*Client, error) {
	var creds credentials.TransportCredentials
	switch {
	case opts.Insecure:
		creds = insecure.NewCredentials()
	default:
		tlsCreds, err := tlsCredentials(opts)
		if err != nil {
			return nil, err
		}
		creds = tlsCreds
	}

	conn, err := grpc.NewClient(opts.Endpoint, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", opts.Endpoint, err)
	}

	return &Client{
		conn:    conn,
		Machine: apxv1.NewMachineServiceClient(conn),
	}, nil
}

// Close tears down the connection.
func (c *Client) Close() error { return c.conn.Close() }

func tlsCredentials(opts Options) (credentials.TransportCredentials, error) {
	caPEM, err := os.ReadFile(opts.CA)
	if err != nil {
		return nil, fmt.Errorf("read ca %s: %w", opts.CA, err)
	}
	certPEM, err := os.ReadFile(opts.Cert)
	if err != nil {
		return nil, fmt.Errorf("read cert %s: %w", opts.Cert, err)
	}
	keyPEM, err := os.ReadFile(opts.Key)
	if err != nil {
		return nil, fmt.Errorf("read key %s: %w", opts.Key, err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse client identity: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse CA certificate")
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   "", // validated against certificate IP/names, not DNS
		MinVersion:   tls.VersionTLS13,
	}), nil
}
