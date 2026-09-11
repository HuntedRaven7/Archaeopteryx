package machine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// Candidate locations for the k0s sysext image, in lookup order.
var k0sSysextPaths = []string{
	"/var/lib/extensions/k0s.raw",
	"/var/lib/k0s/k0s.raw",
}

// Seams for testing the exec-heavy bootstrap orchestration.
var (
	// mergeSysext runs `systemd-sysext merge`.
	mergeSysext = func() error {
		_, err := runOutput("systemd-sysext", "merge")
		return err
	}
	// startController enables and starts the k0s controller unit.
	startController = func() error {
		_, err := runOutput("systemctl", "enable", "--now", "k0scontroller.service")
		return err
	}
	// waitNodeReady blocks until the apiserver reports the node Ready.
	waitNodeReady = waitNodeReadyReal
)

// k0sSysextLocation returns the first existing k0s sysext image path.
func k0sSysextLocation() (string, error) {
	for _, p := range k0sSysextPaths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("k0s sysext image not found (looked in %s); run the k0s update component first",
		strings.Join(k0sSysextPaths, ", "))
}

// k0sMerged reports whether the k0s sysext is currently merged.
func k0sMerged() bool {
	out, err := runOutput("systemd-sysext", "status")
	if err != nil {
		out, err = runOutput("systemd-sysext", "list")
		if err != nil {
			return false
		}
	}
	return strings.Contains(out, "k0s")
}

// SysextStatus returns a human line describing the k0s sysext state.
func SysextStatus() string {
	loc, err := k0sSysextLocation()
	if err != nil {
		return fmt.Sprintf("k0s: %v", err)
	}
	state := "not merged"
	if k0sMerged() {
		state = "merged"
	}
	return fmt.Sprintf("k0s sysext %s (%s)", state, loc)
}

// Bootstrap performs the k0s first-boot: stages the sysext, merges it, starts
// the controller, and waits until the single node reports Ready via the local
// kube-apiserver. progress receives human step lines.
func Bootstrap(ctx context.Context, timeout time.Duration, progress func(string)) error {
	if progress == nil {
		progress = func(string) {}
	}

	loc, err := k0sSysextLocation()
	if err != nil {
		return err
	}
	progress(fmt.Sprintf("k0s sysext image: %s", loc))

	if !k0sMerged() {
		progress("merging k0s sysext...")
		if err := mergeSysext(); err != nil {
			return fmt.Errorf("systemd-sysext merge: %w", err)
		}
	}

	progress("starting k0scontroller.service...")
	if err := startController(); err != nil {
		return fmt.Errorf("enable k0scontroller: %w", err)
	}

	progress("waiting for node Ready...")
	if err := waitNodeReady(ctx, timeout); err != nil {
		return err
	}
	progress("node is Ready")
	return nil
}

// waitNodeReadyReal polls the local apiserver until a node is Ready.
func waitNodeReadyReal(ctx context.Context, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, server, err := localAPIClient()
	if err != nil {
		return err
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		body, err := fetchNodes(waitCtx, client, server)
		if err == nil {
			if ready, err := nodeListReady(body); err == nil && ready {
				return nil
			}
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("node did not reach Ready within %s: %w", timeout, waitCtx.Err())
		case <-ticker.C:
		}
	}
}
