package machine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const featuresFixture = `Component: os
Transfer: root
	Title: Microraptor OS Root
	Description: Updates the root partition
	Path: /
	Source: https://github.com/HuntedRaven7/Microraptor/releases/latest/download/microraptor-root-%v.raw
	Updates from: https://github.com/HuntedRaven7/Microraptor/releases/latest/download/microraptor-root-%v.raw
Transfer: uki
	Title: Microraptor Kernel
	Description: Updates the UKI
	Path: /efi

Component: k0s
Transfer: k0s.raw
	Title: k0s sysext
	Description: Kubernetes runtime
	Path: /var/lib/extensions
	Source: https://github.com/HuntedRaven7/Microraptor/releases/latest/download/k0s-%v.raw
`

func TestParseFeatures(t *testing.T) {
	comps, err := parseFeatures(featuresFixture)
	if err != nil {
		t.Fatalf("parseFeatures: %v", err)
	}
	if len(comps) != 2 {
		t.Fatalf("got %d components, want 2", len(comps))
	}
	os := comps[0]
	if os.Name != "os" || os.Title != "Microraptor OS Root" || os.Description != "Updates the root partition" {
		t.Errorf("unexpected os component: %+v", os)
	}
	if len(os.Paths) != 2 || os.Paths[0] != "/" {
		t.Errorf("os paths = %v", os.Paths)
	}
	if len(os.Sources) != 1 || !strings.Contains(os.Sources[0], "latest/download") {
		t.Errorf("os sources = %v", os.Sources)
	}
	if comps[1].Name != "k0s" {
		t.Errorf("second component = %+v", comps[1])
	}
}

func TestParseFeaturesFieldBeforeComponent(t *testing.T) {
	if _, err := parseFeatures("\tTitle: x\nComponent: a\n"); err == nil {
		t.Fatal("expected error for field before any Component")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"26.08.1", "26.09.0", -1},
		{"26.09.0", "26.08.1", 1},
		{"26.09.0", "26.09.0", 0},
		{"v1.31.1-k0s.0", "v1.31.2-k0s.0", -1},
		{"v1.31.1-k0s.0", "v1.31.1-k0s.1", -1},
		{"1.31.2", "1.31.10", -1}, // numeric segment comparison
		{"%d-alpha", "%d-beta", -1},
		{"26.09.0", "26.09.1", -1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestVersionFromDownloadPath(t *testing.T) {
	cases := []struct{ u, want string }{
		{"https://github.com/x/M/releases/download/v26.09.0/microraptor-root.raw", "26.09.0"},
		{"https://github.com/x/M/releases/download/26.09.0/uki.efi", "26.09.0"},
		{"https://other.example/path", ""},
	}
	for _, tc := range cases {
		if got := versionFromDownloadPath(tc.u); got != tc.want {
			t.Errorf("versionFromDownloadPath(%q) = %q, want %q", tc.u, got, tc.want)
		}
	}
}

func TestResolveLatestVersionRedirect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s, want HEAD", r.Method)
		}
		w.Header().Set("Location", "https://github.com/x/M/releases/download/v26.09.0/microraptor-root.raw")
		w.WriteHeader(http.StatusFound)
	}))
	defer ts.Close()

	src := ts.URL + "/releases/latest/download/microraptor-root-%v.raw"
	got, err := resolveLatestVersion(context.Background(), src)
	if err != nil {
		t.Fatalf("resolveLatestVersion: %v", err)
	}
	if got != "26.09.0" {
		t.Errorf("latest = %q, want 26.09.0", got)
	}
}

func TestResolveLatestVersionNonRedirect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	got, err := resolveLatestVersion(context.Background(), ts.URL+"/file.raw")
	if err != nil || got != "" {
		t.Errorf("got %q, %v; want empty", got, err)
	}
}

func TestSlotVersion(t *testing.T) {
	cases := map[string]string{
		"26.08.0.efi":             "26.08.0",
		"microraptor-26.09.0.efi": "26.09.0",
		"microraptor-ddi.raw":     "",
		"26.08.0-junk.efi":        "",
	}
	for in, want := range cases {
		if got := slotVersion(in); got != want {
			t.Errorf("slotVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildPlanClassification(t *testing.T) {
	origFeatures, origPending := execSysupdateFeatures, execSysupdatePending
	origGetLatest, origVersion := getLatest, CurrentVersionFor
	defer func() {
		execSysupdateFeatures, execSysupdatePending = origFeatures, origPending
		getLatest, CurrentVersionFor = origGetLatest, origVersion
	}()

	execSysupdateFeatures = func() (string, error) { return featuresFixture, nil }
	execSysupdatePending = func() (string, error) { return "staged update awaiting reboot", nil }
	CurrentVersionFor = func(component string) string {
		if component == "os" {
			return "26.08.1"
		}
		return ""
	}
	getLatest = func(ctx context.Context, src string) (string, error) {
		if strings.Contains(src, "k0s") {
			return "", nil
		}
		return "26.09.0", nil
	}

	plan, err := BuildPlan(context.Background())
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if !plan.RebootOwed {
		t.Error("expected reboot owed")
	}
	if len(plan.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(plan.Components))
	}
	var os, k *PlanComponent
	for i := range plan.Components {
		c := &plan.Components[i]
		switch c.Name {
		case "os":
			os = c
		case "k0s":
			k = c
		}
	}
	if os == nil || k == nil {
		t.Fatalf("missing components: %+v", plan.Components)
	}
	if !os.UpdateAvailable || os.Status != "update available" {
		t.Errorf("os component = %+v", os)
	}
	if !os.RebootRequired {
		t.Error("os update should require reboot")
	}
	if !plan.RebootRequired {
		t.Error("plan reboot_required should be set with an OS update present")
	}
	if k.UpdateAvailable {
		t.Errorf("k0s updated even though version unknown: %+v", k)
	}
	if k.RebootRequired {
		t.Error("k0s should not require reboot")
	}
}

func TestPreviousSlot(t *testing.T) {
	origRead, origCurrent := readEFISlots, currentSlot
	defer func() { readEFISlots, currentSlot = origRead, origCurrent }()

	readEFISlots = func() ([]string, error) {
		return []string{"microraptor-26.08.0.efi", "microraptor-26.09.0.efi"}, nil
	}
	currentSlot = func() string { return "26.09.0" }

	prev, err := previousSlot()
	if err != nil {
		t.Fatalf("previousSlot: %v", err)
	}
	if prev != "/efi/EFI/Linux/microraptor-26.08.0.efi" {
		t.Errorf("prev = %q", prev)
	}
}

func TestPreviousSlotSingleEntry(t *testing.T) {
	origRead := readEFISlots
	defer func() { readEFISlots = origRead }()
	readEFISlots = func() ([]string, error) { return []string{"microraptor-26.09.0.efi"}, nil }
	if _, err := previousSlot(); err == nil {
		t.Fatal("expected error with a single UKI")
	}
}
