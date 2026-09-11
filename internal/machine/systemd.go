package machine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Service is one systemd unit entry as reported by ListServices.
type Service struct {
	Name            string
	State           string
	SubState        string
	Description     string
	ActiveState     string
	ActiveEnterUsec int64
}

// systemdUnitJSON is the per-record shape of `systemctl list-units -o json`.
type systemdUnitJSON struct {
	Unit        string `json:"unit"`
	Load        string `json:"load"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Description string `json:"description"`
}

// parseSystemdJSON decodes the `systemctl list-units ... -o json` payload.
func parseSystemdJSON(out string) ([]Service, error) {
	var raw []systemdUnitJSON
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("decode systemctl json: %w", err)
	}
	svcs := make([]Service, 0, len(raw))
	for _, r := range raw {
		if r.Unit == "" || !strings.HasSuffix(r.Unit, ".service") {
			continue
		}
		svcs = append(svcs, Service{
			Name:        r.Unit,
			State:       r.Active,
			SubState:    r.Sub,
			Description: r.Description,
			ActiveState: r.Active,
		})
	}
	return svcs, nil
}

// ListServices returns systemd service units with their load/active state.
// The ActiveEnterTimestampMonotonic timestamps are enriched with a second
// batched `systemctl show` call; failures there degrade gracefully to 0.
func ListServices() ([]Service, error) {
	out, err := runOutput("systemctl",
		"list-units", "--type=service", "--all", "--plain", "--no-legend",
		"--no-pager", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("systemctl list-units: %w", err)
	}
	svcs, err := parseSystemdJSON(out)
	if err != nil {
		return nil, err
	}
	if len(svcs) == 0 {
		return svcs, nil
	}

	names := make([]string, 0, len(svcs))
	for _, s := range svcs {
		names = append(names, s.Name)
	}
	args := append([]string{"show", "--value", "-p", "ActiveEnterTimestampMonotonic", "--no-pager"}, names...)
	if usecOut, err := runOutput("systemctl", args...); err == nil {
		for i, line := range strings.Split(usecOut, "\n") {
			if i >= len(svcs) {
				break
			}
			if v, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64); err == nil {
				svcs[i].ActiveEnterUsec = v
			}
		}
	}
	return svcs, nil
}

// ServiceActionKind is one of start/stop/restart/reload.
type ServiceActionKind string

const (
	ActionStart   ServiceActionKind = "start"
	ActionStop    ServiceActionKind = "stop"
	ActionRestart ServiceActionKind = "restart"
	ActionReload  ServiceActionKind = "reload"
)

// execSystemctlAction runs the systemctl verb; indirected for tests.
var execSystemctlAction = func(unit string, action ServiceActionKind) error {
	_, err := runOutput("systemctl", string(action), unit)
	return err
}

// ServiceAction applies a lifecycle action to a systemd unit and returns a
// human summary.
func ServiceAction(unit string, action ServiceActionKind) (string, error) {
	if !ValidUnit(unit) {
		return "", fmt.Errorf("invalid unit name %q", unit)
	}
	if err := execSystemctlAction(unit, action); err != nil {
		return "", fmt.Errorf("systemctl %s %s: %w", action, unit, err)
	}
	return fmt.Sprintf("%s: %s", unit, action), nil
}

// ValidUnit restricts unit names to systemd-safe characters ending in .service.
func ValidUnit(unit string) bool {
	if unit == "" || !strings.HasSuffix(unit, ".service") || unit == ".service" {
		return false
	}
	for _, r := range unit {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !ok && !strings.ContainsRune("@_.-\\", r) {
			return false
		}
	}
	return true
}

// ParseServiceAction maps a bare verb to a ServiceActionKind.
func ParseServiceAction(verb string) (ServiceActionKind, error) {
	switch verb {
	case "start":
		return ActionStart, nil
	case "stop":
		return ActionStop, nil
	case "restart":
		return ActionRestart, nil
	case "reload":
		return ActionReload, nil
	default:
		return "", fmt.Errorf("unsupported service action %q (want start|stop|restart|reload)", verb)
	}
}
