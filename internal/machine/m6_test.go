package machine

import (
	"context"
	"strings"
	"testing"
	"time"
)

// recordingExec builds a seam that records argv and returns nil.
func recordingExec(rec *[][]string) func(args ...string) error {
	return func(args ...string) error {
		*rec = append(*rec, args)
		return nil
	}
}

func TestEventPredicate(t *testing.T) {
	cases := []struct {
		e    JournalEntry
		want bool
	}{
		{JournalEntry{Unit: "k0scontroller.service", Priority: 6}, true},
		{JournalEntry{Unit: "systemd-sysupdate.service", Priority: 6}, true},
		{JournalEntry{Unit: "apxd.service", Priority: 6}, true},
		{JournalEntry{Unit: "kernel", Priority: 6}, true},
		{JournalEntry{Unit: "sshd.service", Priority: 6}, false},
		{JournalEntry{Unit: "sshd.service", Priority: 3}, true}, // ELOG_PRI_ERR
		{JournalEntry{Unit: "cron.service", Priority: 5}, false},
	}
	for _, tc := range cases {
		if got := EventPredicate(tc.e); got != tc.want {
			t.Errorf("EventPredicate(%+v) = %v, want %v", tc.e, got, tc.want)
		}
	}
}

func TestEventKindFor(t *testing.T) {
	cases := map[string]string{
		"k0scontroller.service":     EventK0s,
		"k0s-worker.service":        EventK0s,
		"systemd-sysupdate.service": EventUpdate,
		"systemd-boot-efi.socket":   EventUpdate,
		"apxd.service":              EventMachine,
		"other.service":             "",
	}
	for unit, want := range cases {
		if got := eventKindFor(unit); got != want {
			t.Errorf("eventKindFor(%q) = %q, want %q", unit, got, want)
		}
	}
}

func TestRebootGraceful(t *testing.T) {
	var rec [][]string
	orig := execSystemctlLifecycle
	execSystemctlLifecycle = recordingExec(&rec)
	defer func() { execSystemctlLifecycle = orig }()

	msg, err := Reboot(RebootModeGraceful)
	if err != nil {
		t.Fatalf("Reboot: %v", err)
	}
	if !strings.Contains(msg, "rebooting") {
		t.Errorf("msg = %q", msg)
	}
	if len(rec) != 1 || strings.Join(rec[0], " ") != "reboot" {
		t.Errorf("argv = %v", rec)
	}
}

func TestRebootPoweroff(t *testing.T) {
	var rec [][]string
	orig := execSystemctlLifecycle
	execSystemctlLifecycle = recordingExec(&rec)
	defer func() { execSystemctlLifecycle = orig }()

	if _, err := Reboot(RebootModePoweroff); err != nil {
		t.Fatalf("Reboot(poweroff): %v", err)
	}
	if len(rec) != 1 || rec[0][0] != "poweroff" {
		t.Errorf("argv = %v", rec)
	}
}

func TestRebootUnknownMode(t *testing.T) {
	if _, err := Reboot("bogus"); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestShutdown(t *testing.T) {
	var rec [][]string
	orig := execSystemctlLifecycle
	execSystemctlLifecycle = recordingExec(&rec)
	defer func() { execSystemctlLifecycle = orig }()

	msg, err := Shutdown()
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !strings.Contains(msg, "powering off") || len(rec) != 1 || rec[0][0] != "poweroff" {
		t.Errorf("msg=%q argv=%v", msg, rec)
	}
}

func TestResetSequence(t *testing.T) {
	var rec [][]string
	origSys, origK0s, origUnmerge, origRemove := execSystemctlLifecycle, k0sReset, unmergeSysext, removeStateDirs
	execSystemctlLifecycle = recordingExec(&rec)
	k0sReset = func() error { rec = append(rec, []string{"k0s", "reset"}); return nil }
	unmergeSysext = func() error { rec = append(rec, []string{"systemd-sysext", "unmerge"}); return nil }
	removeStateDirs = func(wipe bool) error {
		rec = append(rec, []string{"removeStateDirs", boolString(wipe)})
		return nil
	}
	defer func() {
		execSystemctlLifecycle, k0sReset, unmergeSysext, removeStateDirs = origSys, origK0s, origUnmerge, origRemove
	}()

	msg, err := Reset(false)
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if !strings.Contains(msg, "machine config kept") {
		t.Errorf("msg = %q", msg)
	}
	want := []string{
		"stop k0scontroller.service",
		"disable k0scontroller.service",
		"k0s reset",
		"removeStateDirs false",
		"systemd-sysext unmerge",
		"reboot",
	}
	var got []string
	for _, a := range rec {
		got = append(got, strings.Join(a, " "))
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("sequence = %v\nwant %v", got, want)
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestResetWipe(t *testing.T) {
	var wiped bool
	origRemove, origSys := removeStateDirs, execSystemctlLifecycle
	removeStateDirs = func(wipe bool) error {
		wiped = wipe
		return nil
	}
	execSystemctlLifecycle = func(args ...string) error { return nil }
	defer func() { removeStateDirs, execSystemctlLifecycle = origRemove, origSys }()

	msg, err := Reset(true)
	if err != nil {
		t.Fatalf("Reset(wipe): %v", err)
	}
	if !wiped {
		t.Error("wipe not propagated")
	}
	if !strings.Contains(msg, "factory reset") {
		t.Errorf("msg = %q", msg)
	}
}

func TestStreamEventsMapsToNodeEvents(t *testing.T) {
	var got []NodeEvent
	origStream := streamJournalInternal
	streamJournalInternal = func(ctx context.Context, o JournalOptions, emit func(JournalEntry) error) error {
		_ = emit(JournalEntry{
			Timestamp:  time.Now(),
			Hostname:   "node-01",
			Identifier: "k0scontroller",
			Message:    "controller started",
			Unit:       "k0scontroller.service",
			Priority:   6,
		})
		return nil
	}
	defer func() { streamJournalInternal = origStream }()

	if err := StreamEvents(context.Background(), func(e NodeEvent) error {
		got = append(got, e)
		return nil
	}); err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events", len(got))
	}
	if got[0].Type != EventK0s || got[0].Message != "controller started" {
		t.Errorf("event = %+v", got[0])
	}
	if got[0].Metadata["unit"] != "k0scontroller.service" {
		t.Errorf("metadata = %v", got[0].Metadata)
	}
}
