package apid

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
	"github.com/HuntedRaven7/Archaeopteryx/internal/config"
	"github.com/HuntedRaven7/Archaeopteryx/internal/machine"
)

// Config configures the apxd daemon.
type Config struct {
	Listen     string
	StateDir   string
	ConfigPath string
}

// Server is the node-side gRPC control plane; it owns the PKI, the gRPC
// server, and the machine service implementation.
type Server struct {
	cfg     Config
	pki     *config.PKI
	grpc    *grpc.Server
	machine *machineServer
}

// New builds the server, ensuring the node PKI exists.
func New(cfg Config) (*Server, error) {
	if cfg.ConfigPath == "" {
		cfg.ConfigPath = config.DefaultConfigPath
	}
	pki, err := config.EnsurePKI(cfg.StateDir)
	if err != nil {
		return nil, fmt.Errorf("ensure pki: %w", err)
	}
	s := &Server{
		cfg: cfg,
		pki: pki,
		grpc: grpc.NewServer(
			grpc.Creds(newReloadableCreds(cfg.StateDir)),
		),
	}
	s.machine = &machineServer{srv: s}
	return s, nil
}

// Register wires the API implementations onto the server.
func (s *Server) Register() {
	apxv1.RegisterMachineServiceServer(s.grpc, s.machine)
}

// LogMaintenance prints the onboarding token when the node is unconfigured so
// an operator can bootstrap it over the console/SSH.
func (s *Server) LogMaintenance(log *slog.Logger) {
	if s.IsConfigured() {
		return
	}
	tok, err := config.LoadOrCreateOnboardingToken(s.cfg.StateDir)
	if err != nil {
		log.Warn("could not load onboarding token", "error", err)
		return
	}
	log.Info("node is unconfigured; maintenance mode active",
		"onboarding_token", tok,
		"state_dir", s.cfg.StateDir)
}

// PKI exposes the node identity for API consumers and onboarding.
func (s *Server) PKI() *config.PKI { return s.pki }

// ConfigPath returns the machine config path this server manages.
func (s *Server) ConfigPath() string { return s.cfg.ConfigPath }

// StateDir returns the state directory.
func (s *Server) StateDir() string { return s.cfg.StateDir }

// IsConfigured reports whether a machine config has been applied.
func (s *Server) IsConfigured() bool {
	_, err := os.Stat(s.cfg.ConfigPath)
	return err == nil
}

// MaintenanceToken returns the onboarding token, or "" when the node is
// configured or no token is set.
func (s *Server) MaintenanceToken() string {
	if t, err := config.LoadOrCreateOnboardingToken(s.cfg.StateDir); err == nil {
		return t
	}
	return ""
}

// ApplyMachineConfig validates and installs a machine config, swapping the
// trust anchor and server identity and exiting maintenance mode.
func (s *Server) ApplyMachineConfig(cfg *config.MachineConfig) error {
	dir := s.cfg.StateDir

	if err := os.WriteFile(filepath.Join(dir, config.ServerFile), []byte(cfg.API.Cert), 0o600); err != nil {
		return fmt.Errorf("write server cert: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.ServerKey), []byte(cfg.API.Key), 0o600); err != nil {
		return fmt.Errorf("write server key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.CAFile), []byte(cfg.API.CA), 0o644); err != nil {
		return fmt.Errorf("write ca: %w", err)
	}
	// The node never needs to issue certs: drop the bootstrap CA key.
	_ = os.Remove(filepath.Join(dir, config.CAKeyFile))

	if err := cfg.Save(s.cfg.ConfigPath, 0o600); err != nil {
		return fmt.Errorf("save machine config: %w", err)
	}

	if cfg.Hostname != "" {
		if err := machine.SetHostname(cfg.Hostname); err != nil {
			slog.Warn("set hostname", "error", err)
		}
	}

	// Exit maintenance mode.
	if err := os.Remove(filepath.Join(dir, "onboarding-token")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("disable onboarding: %w", err)
	}
	return nil
}

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
