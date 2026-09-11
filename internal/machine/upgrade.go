package machine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PlanComponent describes one updateable component's state.
type PlanComponent struct {
	Name            string
	Title           string
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	RebootRequired  bool
	Status          string
	Message         string
}

// UpdatePlan is the classified set of available updates.
type UpdatePlan struct {
	Components     []PlanComponent
	RebootRequired bool
	RebootOwed     bool
}

// CurrentVersionFor returns the installed version of a component; indirected
// for tests.
var CurrentVersionFor = defaultCurrentVersion

// Reboot exec seams.
var (
	execBootctl    = func(args ...string) error { _, err := runOutput("bootctl", args...); return err }
	readEFISlots   = listEFISlotFiles
	currentSlot    = installedVersion
	execReboot     = func() error { _, err := runOutput("systemctl", "reboot"); return err }
	applyComponent = ApplyUpdate
)

// defaultCurrentVersion derives the installed version: the FSDK point release
// from os-release (or the staged DDI symlink) for OS components, `k0s version`
// for k0s.
func defaultCurrentVersion(component string) string {
	if isK0s(component) {
		if out, err := runOutput("k0s", "version"); err == nil {
			v := strings.Split(out, ",")[0]
			if v = strings.TrimSpace(v); v != "" && v != "k0s" {
				return v
			}
		}
		return ""
	}
	if v := ReadOSRelease()["IMAGE_VERSION"]; v != "" {
		return v
	}
	return installedVersion()
}

func isK0s(component string) bool {
	return strings.Contains(strings.ToLower(component), "k0s")
}

// requiresReboot is true for any component whose payload is consumed at boot
// (root partition, UKI, DDI); k0s sysexts are merged live.
func requiresReboot(component string) bool {
	return !isK0s(component)
}

// BuildPlan assembles and classifies the update plan from live sysupdate
// state.
func BuildPlan(ctx context.Context) (*UpdatePlan, error) {
	comps, err := Features()
	if err != nil {
		return nil, err
	}
	plan := &UpdatePlan{RebootOwed: RebootOwed()}
	for _, c := range comps {
		pc := PlanComponent{Name: c.Name, Title: c.Title}
		pc.CurrentVersion = CurrentVersionFor(c.Name)
		for _, src := range c.Sources {
			if v, err := getLatest(ctx, src); err == nil && v != "" {
				pc.LatestVersion = v
				break
			}
		}
		known := pc.CurrentVersion != "" && pc.LatestVersion != ""
		pc.UpdateAvailable = known && CompareVersions(pc.CurrentVersion, pc.LatestVersion) < 0
		pc.RebootRequired = requiresReboot(c.Name)
		switch {
		case pc.UpdateAvailable:
			pc.Status = "update available"
		case known && !pc.UpdateAvailable:
			pc.Status = "up to date"
			pc.RebootRequired = false
		default:
			pc.Status = "unknown"
		}
		if pc.CurrentVersion != "" && pc.LatestVersion != "" {
			pc.Message = fmt.Sprintf("%s -> %s", pc.CurrentVersion, pc.LatestVersion)
		}
		plan.Components = append(plan.Components, pc)
		if pc.UpdateAvailable {
			plan.RebootRequired = plan.RebootRequired || pc.RebootRequired
		}
	}
	return plan, nil
}

// FormatPlan renders the plan as a fixed-width table for streaming progress.
func FormatPlan(p *UpdatePlan) string {
	var b strings.Builder
	b.WriteString("COMPONENT  CURRENT  LATEST  STATUS\n")
	for _, c := range p.Components {
		fmt.Fprintf(&b, "%-10s  %-8s  %-8s  %s\n",
			c.Name, orDash(c.CurrentVersion), orDash(c.LatestVersion), c.Status)
	}
	return strings.TrimRight(b.String(), "\n")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// UpdateOptions controls the update run.
type UpdateOptions struct {
	Component      string // "all", "os", "k0s", or a specific component name
	CheckOnly      bool
	Reboot         bool
	RebootStrategy string // "manual", "staged", "direct"
	Verify         bool
}

// Update runs the auto-determine flow: plan, classify, apply, reboot policy.
func Update(ctx context.Context, opts UpdateOptions, progress func(string)) (*UpdatePlan, error) {
	if progress == nil {
		progress = func(string) {}
	}
	plan, err := BuildPlan(ctx)
	if err != nil {
		return nil, err
	}
	progress(FormatPlan(plan))
	if opts.CheckOnly {
		progress("check complete")
		return plan, nil
	}

	anyUpdate := false
	for _, c := range plan.Components {
		if !c.UpdateAvailable || !inScope(c.Name, opts.Component) {
			continue
		}
		anyUpdate = true
		if err := applyComponent(ctx, c.Name, opts.Verify, progress); err != nil {
			return plan, fmt.Errorf("update %s: %w", c.Name, err)
		}
	}
	if !anyUpdate {
		progress("up to date")
		return plan, nil
	}

	if plan.RebootRequired {
		if opts.Reboot && opts.RebootStrategy == "direct" {
			progress("rebooting (direct strategy)...")
			if err := execReboot(); err != nil {
				return plan, fmt.Errorf("reboot: %w", err)
			}
		} else if opts.Reboot && opts.RebootStrategy == "manual" {
			progress("update staged; manual reboots is requested — reboot the node to activate")
		} else {
			progress("update staged; reboot required (kured or `apxctl reboot`)")
		}
	}
	return plan, nil
}

func inScope(name, scope string) bool {
	switch scope {
	case "", "all":
		return true
	case "os":
		return !isK0s(name)
	case "k0s":
		return isK0s(name)
	}
	return name == scope
}

// ApplyUpdate stages a component via systemd-sysupdate, then merges the k0s
// sysext when required.
func ApplyUpdate(ctx context.Context, component string, verify bool, progress func(string)) error {
	if _, err := execLookPath("systemd-sysupdate"); err != nil {
		return fmt.Errorf("systemd-sysupdate not available: %w", err)
	}
	progress(fmt.Sprintf("applying %s update...", component))
	if _, err := execSysupdateUpdate(component, verify); err != nil {
		return fmt.Errorf("systemd-sysupdate update: %w", err)
	}
	if isK0s(component) {
		progress("merging k0s sysext...")
		if err := mergeSysext(); err != nil {
			return fmt.Errorf("systemd-sysext merge: %w", err)
		}
		progress("k0s sysext merged; no reboot required")
	} else {
		progress(fmt.Sprintf("%s staged; reboot required to activate", component))
	}
	return nil
}

func listEFISlotFiles() ([]string, error) {
	entries, err := os.ReadDir("/efi/EFI/Linux")
	if err != nil {
		return nil, fmt.Errorf("read /efi/EFI/Linux: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".efi") {
			files = append(files, e.Name())
		}
	}
	return files, nil
}

// slotVersion extracts the version from a UKI/DDI filename, tolerating prefixes.
func slotVersion(base string) string {
	name := base
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".efi")
	name = strings.TrimSuffix(name, ".raw")
	// Strip any trailing alphanumeric prefix ending in '-', e.g. "microraptor-".
	if i := strings.LastIndexByte(name, '-'); i >= 0 {
		name = name[i+1:]
	}
	for _, r := range name {
		if !(r >= '0' && r <= '9' || r == '.') {
			return ""
		}
	}
	return name
}

// previousSlot picks the non-running UKI entry for rollback.
func previousSlot() (string, error) {
	files, err := readEFISlots()
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no UKI entries under /efi/EFI/Linux")
	}
	cur := currentSlot()
	if cur != "" {
		for _, f := range files {
			if slotVersion(f) != cur {
				return filepath.Join("/efi", "EFI", "Linux", f), nil
			}
		}
	}
	// Installed version unknown: fall back to the second-newest entry.
	sort.Slice(files, func(i, j int) bool {
		return CompareVersions(slotVersion(files[i]), slotVersion(files[j])) > 0
	})
	if len(files) < 2 {
		return "", fmt.Errorf("only one UKI entry; nothing to roll back to")
	}
	return filepath.Join("/efi", "EFI", "Linux", files[1]), nil
}

// Rollback forces the previous slot for the next boot via `bootctl
// set-oneshot`, optionally rebooting now.
func Rollback(ctx context.Context, reboot bool) (string, error) {
	prev, err := previousSlot()
	if err != nil {
		return "", err
	}
	if err := execBootctl("set-oneshot", prev); err != nil {
		return "", fmt.Errorf("bootctl set-oneshot %s: %w", prev, err)
	}
	msg := fmt.Sprintf("next boot will use %s (set-oneshot)", prev)
	if reboot {
		if err := execReboot(); err != nil {
			return "", fmt.Errorf("reboot: %w", err)
		}
		msg += "; rebooting now"
	}
	return msg, nil
}
