package apid

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
	"github.com/HuntedRaven7/Archaeopteryx/internal/config"
	"github.com/HuntedRaven7/Archaeopteryx/internal/machine"
	"github.com/HuntedRaven7/Archaeopteryx/pkg/version"
)

// validateServerIdentity ensures the payload identity is a usable server
// certificate signed by the CA carried in the same config.
func validateServerIdentity(cfg *config.MachineConfig) error {
	pair, err := tls.X509KeyPair([]byte(cfg.API.Cert), []byte(cfg.API.Key))
	if err != nil {
		return fmt.Errorf("load cert/key: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(cfg.API.CA)) {
		return fmt.Errorf("parse CA PEM")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return fmt.Errorf("server cert not signed by the configured CA: %w", err)
	}
	return nil
}

// machineServer implements apx.v1.MachineService for the node.
type machineServer struct {
	apxv1.UnimplementedMachineServiceServer
	srv *Server
}

// Version reports the daemon build information.
func (m *machineServer) Version(ctx context.Context, req *apxv1.VersionRequest) (*apxv1.VersionResponse, error) {
	v := version.Current()
	return &apxv1.VersionResponse{
		Version:   v.Version,
		Commit:    v.Commit,
		BuildDate: v.BuildDate,
		GoVersion: v.GoVersion,
		Platform:  v.Platform,
	}, nil
}

// GetStatus gathers host, boot slot, and k0s state.
func (m *machineServer) GetStatus(ctx context.Context, req *apxv1.GetStatusRequest) (*apxv1.GetStatusResponse, error) {
	host, err := machine.CollectHost()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "host info: %v", err)
	}
	kube := machine.CollectKube()
	return &apxv1.GetStatusResponse{
		Hostname:       host.Hostname,
		OsId:           host.OSID,
		OsName:         host.OSName,
		OsPrettyName:   host.OSPrettyName,
		OsVersion:      host.OSVersion,
		OsImageVersion: host.OSImageVersion,
		KernelRelease:  host.KernelRelease,
		BootId:         host.BootID,
		UptimeSeconds:  int64(host.Uptime.Seconds()),
		BootSlot: &apxv1.SlotInfo{
			Current:          host.Slot,
			RunningVersion:   host.RunningVersion,
			InstalledVersion: host.InstalledVersion,
		},
		Kubernetes: &apxv1.KubernetesStatus{
			State:        apxv1.KubernetesStatus_State(int32(kube.State)),
			SysextMerged: kube.SysextMerged,
			Version:      kube.Version,
			NodeName:     kube.NodeName,
			Role:         kube.Role,
			Ready:        kube.Ready,
		},
	}, nil
}

// GetConfig returns the applied machine configuration with secrets masked, or
// NotFound when the node has not been configured yet.
func (m *machineServer) GetConfig(ctx context.Context, req *apxv1.GetConfigRequest) (*apxv1.GetConfigResponse, error) {
	cfg, err := config.LoadMachineConfig(m.srv.ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound, "node is not configured; use apply-config")
		}
		return nil, status.Errorf(codes.Internal, "read machine config: %v", err)
	}
	return &apxv1.GetConfigResponse{Config: cfg.Redacted()}, nil
}

// ApplyConfig installs a machine config while the node is unconfigured
// (maintenance mode), gated by the onboarding token when one is set.
func (m *machineServer) ApplyConfig(ctx context.Context, req *apxv1.ApplyConfigRequest) (*apxv1.ApplyConfigResponse, error) {
	if m.srv.IsConfigured() {
		return nil, status.Error(codes.AlreadyExists, "node is already configured")
	}

	cfg, err := config.ParseMachineConfig([]byte(req.Config))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	// Verify the incoming server identity is usable and signed by the CA in
	// the config before we trust it over the wire.
	if err := validateServerIdentity(cfg); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "server identity: %v", err)
	}

	if tok := m.srv.MaintenanceToken(); tok != "" {
		if req.MaintenanceToken != tok {
			return nil, status.Error(codes.PermissionDenied, "invalid maintenance token")
		}
	}

	if err := m.srv.ApplyMachineConfig(cfg); err != nil {
		return nil, status.Errorf(codes.Internal, "apply config: %v", err)
	}

	return &apxv1.ApplyConfigResponse{
		Message: "machine config applied; node left maintenance mode (use the generated client bundle to reconnect)",
	}, nil
}
