package machine

import "fmt"

// SetHostname applies a static hostname through systemd-hostnamed.
func SetHostname(name string) error {
	if name == "" {
		return nil
	}
	if _, err := runOutput("hostnamectl", "set-hostname", name); err != nil {
		return fmt.Errorf("hostnamectl set-hostname: %w", err)
	}
	return nil
}
