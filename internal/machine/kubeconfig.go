package machine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Kubeconfig paths for a root-level k0s controller.
const (
	k0sAdminConf = "/var/lib/k0s/pki/admin.conf"
)

// kubeContext is the minimal shape of a kubeconfig entry we rewrite for merge.
type kubeconfig struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Clusters   []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			ClientCertificateData string `yaml:"client-certificate-data"`
			ClientKeyData         string `yaml:"client-key-data"`
		} `yaml:"user"`
	} `yaml:"users"`
}

// parseKubeconfig decodes a kubeconfig document.
func parseKubeconfig(b []byte) (*kubeconfig, error) {
	var kc kubeconfig
	if err := yaml.Unmarshal(b, &kc); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	if len(kc.Clusters) == 0 || len(kc.Users) == 0 || len(kc.Contexts) == 0 {
		return nil, fmt.Errorf("kubeconfig is incomplete")
	}
	return &kc, nil
}

// localAPIClient builds an HTTPS client for the local kube-apiserver from the
// k0s admin.conf, pinned to the cluster CA embedded in it. It also returns the
// apiserver URL to target.
func localAPIClient() (*http.Client, string, error) {
	b, err := os.ReadFile(k0sAdminConf)
	if err != nil {
		return nil, "", fmt.Errorf("read %s (is k0s initialized?): %w", k0sAdminConf, err)
	}
	kc, err := parseKubeconfig(b)
	if err != nil {
		return nil, "", err
	}
	cluster := kc.Clusters[0].Cluster
	user := kc.Users[0].User

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	pool := x509.NewCertPool()
	if cluster.CertificateAuthorityData != "" {
		if !pool.AppendCertsFromPEM([]byte(cluster.CertificateAuthorityData)) {
			return nil, "", fmt.Errorf("invalid certificate-authority-data in %s", k0sAdminConf)
		}
		tlsCfg.RootCAs = pool
		tlsCfg.ServerName = "kubernetes"
	}
	if user.ClientCertificateData != "" && user.ClientKeyData != "" {
		cert, err := tls.X509KeyPair([]byte(user.ClientCertificateData), []byte(user.ClientKeyData))
		if err != nil {
			return nil, "", fmt.Errorf("invalid client identity in %s: %w", k0sAdminConf, err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	base := cluster.Server
	if base == "" {
		base = "https://127.0.0.1:6443"
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
			DialContext: (&net.Dialer{Timeout: 5 * time.Second}).
				DialContext,
		},
	}, cluster.Server, nil
}

// fetchNodes GETs the node list from the apiserver.
func fetchNodes(ctx context.Context, client *http.Client, server string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/api/v1/nodes", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apiserver %s: %s", server, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// nodeListReady reports whether any node in a NodeList JSON has Ready=True.
func nodeListReady(body []byte) (bool, error) {
	var list struct {
		Items []struct {
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return false, fmt.Errorf("parse node list: %w", err)
	}
	for _, n := range list.Items {
		for _, c := range n.Status.Conditions {
			if c.Type == "Ready" && c.Status == "True" {
				return true, nil
			}
		}
	}
	return false, nil
}
