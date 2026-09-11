package machine

import "testing"

func TestSlotFromCmdline(t *testing.T) {
	cases := []struct {
		name    string
		cmdline string
		want    string
	}{
		{"label boot", "quiet root=LABEL=Microraptor-root-a ro", "Microraptor-root-a"},
		{"label boot b", "root=LABEL=Microraptor-root-b", "Microraptor-root-b"},
		{"uuid in path", "root=/dev/disk/by-partuuid/abc Microraptor-root-a", ""},
		{"no root", "quiet splash", ""},
		{"partuuid root", "quiet rw root=PARTUUID=deadbeef ro", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := slotFromCmdline(tc.cmdline); got != tc.want {
				t.Fatalf("slotFromCmdline(%q) = %q, want %q", tc.cmdline, got, tc.want)
			}
		})
	}
}

func TestVersionFromTarget(t *testing.T) {
	cases := []struct {
		name   string
		target string
		want   string
	}{
		{"plain", "/var/lib/extensions/microraptor-ddi-26.08.0.raw", "26.08.0"},
		{"relative", "microraptor-ddi-1.2.3.raw", "1.2.3"},
		{"no version", "microraptor-ddi-rollback.raw", ""},
		{"prefixed", "/efi/EFI/Linux/microraptor-26.08.0.efi", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionFromTarget(tc.target); got != tc.want {
				t.Fatalf("versionFromTarget(%q) = %q, want %q", tc.target, got, tc.want)
			}
		})
	}
}

func TestParseOSRelease(t *testing.T) {
	content := "NAME=microraptor\nID=flatcar\nVERSION=\"4593.2.3-fsdk\"\nVERSION_ID=4593.2.3\nIMAGE_VERSION=26.08.0\n# a comment\n\n"
	vals := parseOSReleaseContent(content)
	if vals["ID"] != "flatcar" {
		t.Errorf("ID = %q, want flatcar", vals["ID"])
	}
	if vals["VERSION"] != "4593.2.3-fsdk" {
		t.Errorf("VERSION = %q, want 4593.2.3-fsdk", vals["VERSION"])
	}
	if vals["IMAGE_VERSION"] != "26.08.0" {
		t.Errorf("IMAGE_VERSION = %q, want 26.08.0", vals["IMAGE_VERSION"])
	}
}

func TestUnquote(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"a b"`, "a b"},
		{`'a b'`, "a b"},
		{`plain`, "plain"},
		{`"unbalanced`, `"unbalanced`},
	}
	for _, tc := range cases {
		if got := unquote(tc.in); got != tc.want {
			t.Errorf("unquote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
