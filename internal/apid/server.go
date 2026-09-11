package apid

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
	"github.com/HuntedRaven7/Archaeopteryx/internal/config"
)

// Config configures the apxd daemon.
type Config struct {
	Listen   string
	StateDir string
}

// Server is the node-side gRPC control plane; it owns the PKI, the gRPC
// server, and the machine service implementation.
type Server struct {
	cfg  Config
	pki  *config.PKI
	grpc *grpc.Server
}

// New builds the server, ensuring the node PKI exists.
func New(cfg Config) (*Server, error) {
	pki, err := config.EnsurePKI(cfg.StateDir)
	if err != nil {
		return nil, fmt.Errorf("ensure pki: %w", err)
	}
	creds, err := serverCredentials(pki)
	if err != nil {
		return nil, fmt.Errorf("build tls: %w", err)
	}
	return &Server{
		cfg: cfg,
		pki: pki,
		grpc: grpc.NewServer(
			grpc.Creds(creds),
		),
	}, nil
}

// Register wires the API implementations onto the server.
func (s *Server) Register() {
	apxv1.RegisterMachineServiceServer(s.grpc, &machineServer{})
}

// PKI exposes the node identity for API consumers and onboarding.
func (s *Server) PKI() *config.PKI { return s.pki }

// Serve listens on cfg.Listen and serves gRPC until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	lis, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Listen, err)
	}
	defer lis.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.grpc.Serve(lis)
	}()

	select {
	case <-ctx.Done():
		s.grpc.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}

// serverCredentials derives mTLS credentials: the server presents a cert
// signed by the node CA and requires clients to present a cert from the
// same CA.
func serverCredentials(pki *config.PKI) (credentials.TransportCredentials, error) {
	serverCert, err := tls.LoadX509KeyPair(
		filepath.Join(pki.Dir(), config.ServerFile),
		filepath.Join(pki.Dir(), config.ServerKey),
	)
	if err != nil {
		return nil, fmt.Errorf("load server identity: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pki.CAPEM()) {
		return nil, fmt.Errorf("parse CA certificate")
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}), nil
}
