package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath is where apxd stores the applied machine configuration.
const DefaultConfigPath = "/var/lib/archaeopteryx/config.yaml"

// Version pins the machine configuration schema.
const version = "v1alpha1"

// MachineConfig is the apxconfig machine configuration applied to a node.
type MachineConfig struct {
	Version     string      `yaml:"version"`
	Hostname    string      `yaml:"hostname,omitempty"`
	API         API         `yaml:"api"`
	Maintenance Maintenance `yaml:"maintenance,omitempty"`
	Update      Update      `yaml:"update,omitempty"`
}

// API carries the node API endpoints and mTLS identity material.
type API struct {
	Endpoints []string `yaml:"endpoints,omitempty"`
	CA        string   `yaml:"ca,omitempty"`
	Cert      string   `yaml:"cert,omitempty"`
	Key       string   `yaml:"key,omitempty"`
}

// Maintenance gates onboarding while a node is unconfigured.
type Maintenance struct {
	Token string `yaml:"token,omitempty"`
}

// Update configures how apxctl update proceeds (consumed by a later milestone).
type Update struct {
	Channel        string `yaml:"channel,omitempty"`
	RebootStrategy string `yaml:"reboot_strategy,omitempty"`
	AutoStage      bool   `yaml:"auto_stage,omitempty"`
	Verify         string `yaml:"verify,omitempty"`
}

// LoadMachineConfig reads and validates the config at path.
func LoadMachineConfig(path string) (*MachineConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseMachineConfig(b)
}

// ParseMachineConfig parses and validates config YAML.
func ParseMachineConfig(b []byte) (*MachineConfig, error) {
	var cfg MachineConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse machine config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks structural invariants of the config.
func (c *MachineConfig) Validate() error {
	if c.Version != version {
		return fmt.Errorf("unsupported config version %q (want %q)", c.Version, version)
	}
	if len(c.API.Endpoints) == 0 {
		return errors.New("config must declare at least one api.endpoint")
	}
	if c.API.CA == "" {
		return errors.New("config must include api.ca")
	}
	if c.API.Cert == "" || c.API.Key == "" {
		return errors.New("config must include api.cert and api.key (server identity)")
	}
	return nil
}

// Save atomically writes the config (and any sibling identity files) to path.
func (c *MachineConfig) Save(path string, perm os.FileMode) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".apxconfig-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Redacted renders the config with private key material masked, safe to
// return over the API.
func (c *MachineConfig) Redacted() string {
	copy := *c
	copy.API.Key = "[REDACTED]"
	copy.Maintenance.Token = "[REDACTED]"
	out := &bytes.Buffer{}
	enc := yaml.NewEncoder(out)
	enc.SetIndent(2)
	_ = enc.Encode(&copy)
	enc.Close()
	return out.String()
}
