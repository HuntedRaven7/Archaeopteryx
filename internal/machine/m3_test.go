package machine

import (
	"errors"
	"testing"
	"time"
)

func TestParseSystemdJSON(t *testing.T) {
	in := `[{"unit":"accounts-daemon.service","load":"loaded","active":"active","sub":"running","description":"Accounts Service"},
{"unit":"k0scontroller.service","load":"loaded","active":"inactive","sub":"dead","description":"k0s controller"},
{"unit":"sshd.service","load":"loaded","active":"active","sub":"running","description":"OpenSSH"},
{"unit":"apx-proxy.timer","load":"loaded","active":"active","sub":"waiting","description":"not a service"}]`
	svcs, err := parseSystemdJSON(in)
	if err != nil {
		t.Fatalf("parseSystemdJSON: %v", err)
	}
	if len(svcs) != 3 {
		t.Fatalf("got %d services, want 3", len(svcs))
	}
	if svcs[0].Name != "accounts-daemon.service" || svcs[0].State != "active" || svcs[0].SubState != "running" {
		t.Errorf("unexpected first service: %+v", svcs[0])
	}
	if svcs[1].ActiveState != "inactive" || svcs[1].Description != "k0s controller" {
		t.Errorf("unexpected second service: %+v", svcs[1])
	}
}

func TestParseSystemdJSONBad(t *testing.T) {
	if _, err := parseSystemdJSON("not json"); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestValidUnit(t *testing.T) {
	valid := []string{"sshd.service", "k0scontroller.service", "systemd-journald.service", "user@1000.service"}
	for _, u := range valid {
		if !ValidUnit(u) {
			t.Errorf("ValidUnit(%q) = false, want true", u)
		}
	}
	invalid := []string{"", ".service", "sshd", "../sshd.service", "sshd.service;rm", "sshd service", "sshd.Service"}
	for _, u := range invalid {
		if ValidUnit(u) {
			t.Errorf("ValidUnit(%q) = true, want false", u)
		}
	}
}

func TestParseServiceAction(t *testing.T) {
	for _, v := range []string{"start", "stop", "restart", "reload"} {
		if _, err := ParseServiceAction(v); err != nil {
			t.Errorf("ParseServiceAction(%q): %v", v, err)
		}
	}
	if _, err := ParseServiceAction("nuke"); err == nil {
		t.Error("expected error for unsupported action")
	}
}

func TestServiceActionStub(t *testing.T) {
	orig := execSystemctlAction
	defer func() { execSystemctlAction = orig }()

	called := ""
	execSystemctlAction = func(unit string, a ServiceActionKind) error {
		called = string(a) + " " + unit
		return nil
	}

	got, err := ServiceAction("sshd.service", ActionRestart)
	if err != nil {
		t.Fatalf("ServiceAction: %v", err)
	}
	if called != "restart sshd.service" {
		t.Errorf("ran %q, want \"restart sshd.service\"", called)
	}
	if got != "sshd.service: restart" {
		t.Errorf("message = %q", got)
	}

	execSystemctlAction = func(unit string, a ServiceActionKind) error {
		return errors.New("unit not found")
	}
	if _, err := ServiceAction("sshd.service", ActionStart); err == nil {
		t.Error("expected error from failed systemctl")
	}
}

func TestJournalArgs(t *testing.T) {
	opts := JournalOptions{Unit: "k0scontroller.service", Follow: true, TailLines: 50, Since: "10 minutes ago"}
	got := journalArgs(opts)
	want := []string{"-o", "json", "--no-pager", "--quiet", "-n", "50", "-u", "k0scontroller.service", "-f", "--since", "10 minutes ago"}
	if len(got) != len(want) {
		t.Fatalf("journalArgs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("journalArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Default tail applies when the caller passes 0.
	if tail := (JournalOptions{Follow: true}).tail(); tail != defaultTail {
		t.Errorf("default tail = %d, want %d", tail, defaultTail)
	}
}

func TestParseJournalLineAndFormat(t *testing.T) {
	line := `{"_SOURCE_REALTIME_TIMESTAMP":"1720000000123456","_HOSTNAME":"node1","SYSLOG_IDENTIFIER":"sshd","_PID":"123","MESSAGE":"accepted publickey"}`
	e, err := parseJournalLine([]byte(line))
	if err != nil {
		t.Fatalf("parseJournalLine: %v", err)
	}
	if e.Hostname != "node1" || e.Identifier != "sshd" || e.PID != 123 || e.Message != "accepted publickey" {
		t.Errorf("entry = %+v", e)
	}
	wantTS, _ := time.Parse("2006-01-02T15:04:05.000000-07:00", "2024-07-03T09:46:40.123456+00:00")
	if !e.Timestamp.Equal(wantTS) {
		t.Errorf("timestamp = %v, want %v", e.Timestamp, wantTS)
	}

	got := FormatJournalLine(e)
	want := e.Timestamp.Format("2006-01-02T15:04:05.000000-07:00") + " node1 sshd[123]: accepted publickey"
	if got != want {
		t.Errorf("formatted = %q, want %q", got, want)
	}

	// No-identifier entry still renders message.
	if got := FormatJournalLine(JournalEntry{Message: "bare"}); got != "bare" {
		t.Errorf("bare message = %q", got)
	}
}

func TestParseJournalLineFallsBackToRealtime(t *testing.T) {
	line := `{"__REALTIME_TIMESTAMP":"1720000000123456","SYSLOG_IDENTIFIER":"x","MESSAGE":"m"}`
	e, err := parseJournalLine([]byte(line))
	if err != nil {
		t.Fatalf("parseJournalLine: %v", err)
	}
	if e.Timestamp.IsZero() {
		t.Error("expected realtime timestamp fallback")
	}
}

func TestParseJournalLineBadJSON(t *testing.T) {
	if _, err := parseJournalLine([]byte("not-json{")); err == nil {
		t.Fatal("expected error for invalid json")
	}
}
