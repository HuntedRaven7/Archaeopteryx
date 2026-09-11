package config

import (
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultAPXPort is assumed when an endpoint omits a port.
const DefaultAPXPort = "50000"

// Generated is the offline output of `apxctl gen config`.
type Generated struct {
	CA           []byte
	ServerCert   []byte
	ServerKey    []byte
	AdminCert    []byte
	AdminKey     []byte
	ApxConfig    []byte
	ClientBundle []byte
}

// GenerateCluster creates a fresh cluster CA plus server and admin identities,
// and renders an apxconfig for the node and a client bundle for the operator.
// It runs entirely offline on the workstation.
func GenerateCluster(cluster, endpoint, hostname string) (*Generated, error) {
	endpoint = NormalizeEndpoint(endpoint)

	now := time.Now()
	capem, _, ca, err := createCA("apx-"+cluster+"-ca", caTTLDays, now)
	if err != nil {
		return nil, err
	}

	ips, dns := parseSANs(endpoint)
	serverCert, serverKey, err := createSignedCert(ca, "apxd", nil, x509.ExtKeyUsageServerAuth, caTTLDays, now, ips, dns)
	if err != nil {
		return nil, err
	}
	adminCert, adminKey, err := createSignedCert(ca, "admin", []string{"system:masters"}, x509.ExtKeyUsageClientAuth, caTTLDays, now, nil, nil)
	if err != nil {
		return nil, err
	}

	cfg := &MachineConfig{
		Version:  version,
		Hostname: hostname,
		API: API{
			Endpoints: []string{endpoint},
			CA:        strings.TrimSpace(string(capem)),
			Cert:      strings.TrimSpace(string(serverCert)),
			Key:       strings.TrimSpace(string(serverKey)),
		},
		Update: Update{
			Channel:        "stable",
			RebootStrategy: "staged",
			AutoStage:      true,
			Verify:         "required",
		},
	}
	apxConfig, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	bundle := &ClientBundle{
		Version: version,
		Contexts: map[string]*BundleContext{
			cluster: {
				Endpoint: endpoint,
				CA:       strings.TrimSpace(string(capem)),
				Cert:     strings.TrimSpace(string(adminCert)),
				Key:      strings.TrimSpace(string(adminKey)),
			},
		},
	}
	bundleBytes, err := yaml.Marshal(bundle)
	if err != nil {
		return nil, err
	}

	return &Generated{
		CA:           capem,
		ServerCert:   serverCert,
		ServerKey:    serverKey,
		AdminCert:    adminCert,
		AdminKey:     adminKey,
		ApxConfig:    apxConfig,
		ClientBundle: bundleBytes,
	}, nil
}

// WriteArtifacts writes the generated artifacts into outDir with fixed
// well-known names and returns a list of written file paths.
func (g *Generated) WriteArtifacts(outDir string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	files := map[string][]byte{
		"apxconfig.yaml": g.ApxConfig,
		"apxctl.yaml":    g.ClientBundle,
		"ca.crt":         g.CA,
		"admin.crt":      g.AdminCert,
		"admin.key":      g.AdminKey,
	}
	var written []string
	for name, data := range files {
		path := filepath.Join(outDir, name)
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".key") {
			mode = 0o600
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		written = append(written, path)
	}
	return written, nil
}

// NormalizeEndpoint coerces "10.0.0.5", "10.0.0.5:50000", and
// "https://10.0.0.5:50000" into "https://host:port".
func NormalizeEndpoint(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	e := in
	if !strings.Contains(e, "://") {
		e = "https://" + e
	}
	u, err := url.Parse(e)
	if err != nil {
		return in
	}
	if u.Port() == "" {
		u.Host = u.Host + ":" + DefaultAPXPort
	}
	u.Scheme = "https"
	return u.String()
}

func parseSANs(endpoint string) ([]net.IP, []string) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, nil
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	return nil, []string{host}
}

// ClientBundle is the per-context client configuration consumed by apxctl
// (shaped after the talosconfig convention).
type ClientBundle struct {
	Version  string                    `yaml:"version"`
	Contexts map[string]*BundleContext `yaml:"contexts"`
}

// BundleContext identifies one node endpoint with its mTLS material.
type BundleContext struct {
	Endpoint string `yaml:"endpoint"`
	CA       string `yaml:"ca"`
	Cert     string `yaml:"cert"`
	Key      string `yaml:"key"`
}

// First returns the first context in map order (M2: single-context bundles).
func (b *ClientBundle) First() *BundleContext {
	for _, c := range b.Contexts {
		return c
	}
	return nil
}

// ClientBundleFromContexts builds a bundle from raw identity PEMs.
func ClientBundleFromContexts(ctxs map[string]*BundleContext) ([]byte, error) {
	return yaml.Marshal(&ClientBundle{Version: version, Contexts: ctxs})
}

// ParseClientBundle reads and validates a bundle YAML document.
func ParseClientBundle(b []byte) (*ClientBundle, error) {
	var bd ClientBundle
	if err := yaml.Unmarshal(b, &bd); err != nil {
		return nil, fmt.Errorf("parse client bundle: %w", err)
	}
	if bd.Version != version {
		return nil, fmt.Errorf("unsupported client bundle version %q", bd.Version)
	}
	return &bd, nil
}
