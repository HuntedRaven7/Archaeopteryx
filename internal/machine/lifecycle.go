package machine

import (
	"errors"
	"fmt"
	"os"
)

// Reboot modes.
const (
	RebootModeGraceful = "graceful"
	RebootModePoweroff = "poweroff"
)

// Lifecycle exec seams (tests swap these).
var (
	execSystemctlLifecycle = func(args ...string) error {
		_, err := runOutput("systemctl", args...)
		return err
	}
	k0sReset        = func() error { _, err := runOutput("k0s", "reset"); return err }
	unmergeSysext   = func() error { _, err := runOutput("systemd-sysext", "unmerge"); return err }
	removeStateDirs = defaultRemoveStateDirs
)

// defaultRemoveStateDirs clears node state. Without wipe only the k0s cluster
// state is removed; with wipe the machine config and TLS identity go too so
// the node returns to maintenance mode on next boot.
func defaultRemoveStateDirs(wipe bool) error {
	dirs := []string{"/var/lib/k0s"}
	if wipe {
		dirs = append(dirs, "/var/lib/archaeopteryx")
	}
	var errs []error
	for _, d := range dirs {
		if err := os.RemoveAll(d); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d, err))
		}
	}
	return errors.Join(errs...)
}

// Reboot reboots the node gracefully (systemd stop ordering) or powers it off.
func Reboot(mode string) (string, error) {
	switch mode {
	case "", RebootModeGraceful:
		if err := execSystemctlLifecycle("reboot"); err != nil {
			return "", fmt.Errorf("systemctl reboot: %w", err)
		}
		return "rebooting now", nil
	case RebootModePoweroff:
		if err := execSystemctlLifecycle("poweroff"); err != nil {
			return "", fmt.Errorf("systemctl poweroff: %w", err)
		}
		return "powering off now", nil
	}
	return "", fmt.Errorf("unknown reboot mode %q (graceful|poweroff)", mode)
}

// Shutdown powers the node off.
func Shutdown() (string, error) {
	if err := execSystemctlLifecycle("poweroff"); err != nil {
		return "", fmt.Errorf("systemctl poweroff: %w", err)
	}
	return "powering off now", nil
}

// Reset stops k0s, removes its state (and the machine config with wipe), then
// reboots. Without wipe the node keeps its apx identity; with wipe it boots
// back into maintenance mode.
func Reset(wipe bool) (string, error) {
	// 1. stop + disable the controller (tolerated when k0s is not deployed).
	_ = execSystemctlLifecycle("stop", "k0scontroller.service")
	_ = execSystemctlLifecycle("disable", "k0scontroller.service")
	// 2. `k0s reset` clears cluster files on disk (tolerated when absent).
	_ = k0sReset()
	// 3. wipe the state directories.
	if err := removeStateDirs(wipe); err != nil {
		return "", fmt.Errorf("remove state dirs: %w", err)
	}
	// 4. unmerge sysext layers so the next boot starts clean.
	_ = unmergeSysext()
	msg := "k0s reset; node rebooting (machine config kept)"
	if wipe {
		msg = "factory reset: k0s, machine config, and TLS state removed; node rebooting into maintenance mode"
	}
	// 5. reboot the node.
	if err := execSystemctlLifecycle("reboot"); err != nil {
		return "", fmt.Errorf("reboot: %w", err)
	}
	return msg, nil
}
