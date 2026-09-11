// Package machine provides host status, A/B slot, k0s, and update
// functionality used by the apxd daemon.
package machine

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// execLookPath and execCommand are variable so tests can stub them.
var (
	execLookPath = exec.LookPath
	execCommand  = exec.Command
)

// runOutput runs cmd with args and returns trimmed stdout on success.
func runOutput(cmd string, args ...string) (string, error) {
	if _, err := execLookPath(cmd); err != nil {
		return "", err
	}
	out, err := execCommand(cmd, args...).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		if s != "" {
			err = fmt.Errorf("%w: %s", err, s)
		}
		return "", err
	}
	return s, nil
}

// Host is the runtime status of the host OS.
type Host struct {
	Hostname         string
	OSID             string
	OSName           string
	OSPrettyName     string
	OSVersion        string
	OSImageVersion   string
	KernelRelease    string
	BootID           string
	Uptime           time.Duration
	Slot             string
	RunningVersion   string
	InstalledVersion string
}

// K0sState mirrors the apx.v1 K0sStatus state enum.
type K0sState int32

const (
	K0sStateUnspecified K0sState = 0
	K0sStateDisabled    K0sState = 1
	K0sStateStopped     K0sState = 2
	K0sStateActivating  K0sState = 3
	K0sStateActive      K0sState = 4
	K0sStateFailed      K0sState = 5
)

// KubeStatus is the runtime status of the k0s controller on the node.
type KubeStatus struct {
	State        K0sState
	SysextMerged bool
	Version      string
	NodeName     string
	Role         string
	Ready        bool
}

// CollectHost gathers host facts from procfs and os-release.
func CollectHost() (Host, error) {
	h := Host{}
	rel := ReadOSRelease()
	h.OSID = rel["ID"]
	h.OSName = rel["NAME"]
	h.OSPrettyName = rel["PRETTY_NAME"]
	h.OSVersion = rel["VERSION_ID"]
	h.OSImageVersion = rel["IMAGE_VERSION"]

	if hn, err := os.Hostname(); err == nil {
		h.Hostname = hn
	}
	if k, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		h.BootID = strings.TrimSpace(string(k))
	}
	if k, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		h.KernelRelease = strings.TrimSpace(string(k))
	}
	if u, err := os.ReadFile("/proc/uptime"); err == nil {
		secs, _ := strconv.ParseFloat(strings.Fields(string(u))[0], 64)
		h.Uptime = time.Duration(secs * float64(time.Second))
	}

	h.Slot = slotFromCmdline(readProcCmdline())
	h.RunningVersion = h.OSImageVersion
	h.InstalledVersion = installedVersion()
	if h.InstalledVersion == "" {
		h.InstalledVersion = h.RunningVersion
	}
	return h, nil
}

func readProcCmdline() string {
	b, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return ""
	}
	return string(b)
}

// slotFromCmdline inspects the kernel command line for the root GPT partition
// label (e.g. Microraptor-root-a) used by the A/B scheme.
func slotFromCmdline(cmdline string) string {
	fields := strings.Fields(cmdline)
	for _, f := range fields {
		if strings.HasPrefix(f, "root=LABEL=") {
			return strings.TrimPrefix(f, "root=LABEL=")
		}
	}
	for _, f := range fields {
		if !strings.HasPrefix(f, "root=") {
			continue
		}
		dev := strings.TrimPrefix(f, "root=")
		if i := strings.Index(dev, "Microraptor"); i >= 0 {
			return dev[i:]
		}
	}
	return ""
}

// installedVersion reads the version out of the staged DDI symlink target,
// e.g. ".../microraptor-ddi.raw" -> ".../microraptor-ddi-26.08.0.raw".
func installedVersion() string {
	target, err := os.Readlink("/var/lib/extensions/microraptor-ddi.raw")
	if err != nil {
		return ""
	}
	return versionFromTarget(target)
}

// versionFromTarget extracts the version component from a sysupdate target
// file name. Returns "" when the name does not encode a plain version.
func versionFromTarget(target string) string {
	base := target
	if i := strings.LastIndex(target, "/"); i >= 0 {
		base = target[i+1:]
	}
	base = strings.TrimPrefix(base, "microraptor-ddi-")
	base = strings.TrimSuffix(base, ".raw")
	for _, r := range base {
		if !(r >= '0' && r <= '9' || r == '.') {
			return ""
		}
	}
	if base == "" {
		return ""
	}
	return base
}

// CollectKube probes the k0s sysext and controller without talking to the
// cluster (cluster-aware readiness arrives with the bootstrap milestone).
func CollectKube() KubeStatus {
	info := KubeStatus{}

	if out, err := runOutput("systemd-sysext", "status"); err == nil {
		info.SysextMerged = strings.Contains(out, "k0s")
	} else if out, err := runOutput("systemd-sysext", "list"); err == nil {
		info.SysextMerged = strings.Contains(out, "k0s")
	}

	state, sub := unitActiveState("k0scontroller.service")
	switch state {
	case "":
		info.State = K0sStateDisabled
	case "active":
		info.State = K0sStateActive
	case "activating":
		info.State = K0sStateActivating
	case "inactive", "deactivating":
		info.State = K0sStateStopped
	case "failed":
		info.State = K0sStateFailed
	default:
		info.State = K0sStateUnspecified
	}
	_ = sub

	if info.State == K0sStateActive {
		if v, err := runOutput("k0s", "version"); err == nil {
			info.Version = strings.TrimSpace(strings.Split(v, ",")[0])
		}
		if hn, err := os.Hostname(); err == nil {
			info.NodeName = hn
		}
		info.Role = "controller+worker"
	}
	return info
}

// unitActiveState returns the ActiveState and SubState of a systemd unit,
// or empty strings if systemctl is unavailable or the unit is unknown.
func unitActiveState(unit string) (string, string) {
	out, err := runOutput("systemctl",
		"show", unit, "-p", "ActiveState", "-p", "SubState", "--value")
	if err != nil {
		return "", ""
	}
	lines := strings.Split(out, "\n")
	state := strings.TrimSpace(lines[0])
	sub := ""
	if len(lines) > 1 {
		sub = strings.TrimSpace(lines[1])
	}
	return state, sub
}
