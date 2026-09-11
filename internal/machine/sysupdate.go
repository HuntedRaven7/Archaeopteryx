package machine

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// SysupdateComponent is one `systemd-sysupdate features` section with its
// transfers aggregated.
type SysupdateComponent struct {
	Name        string
	Title       string
	Description string
	Paths       []string
	Sources     []string
}

// Systemd-sysupdate exec seams (tests swap these).
var (
	execSysupdateFeatures = func() (string, error) {
		return runOutput("systemd-sysupdate", "features")
	}
	execSysupdatePending = func() (string, error) {
		return runOutput("systemd-sysupdate", "pending")
	}
	execSysupdateUpdate = func(component string, verify bool) (string, error) {
		var args []string
		if verify {
			args = append(args, "--verify=yes")
		}
		args = append(args, "--component="+component, "update")
		return runOutput("systemd-sysupdate", args...)
	}
	// getLatest resolves the newest version string for a source URL.
	getLatest = func(ctx context.Context, source string) (string, error) {
		return resolveLatestVersion(ctx, source)
	}
)

// parseFeatures decodes the text output of `systemd-sysupdate features`.
// Format (v251+): one or more "Component: <name>" sections, each containing
// indented key/value fields and nested "Transfer: <name>" blocks.
func parseFeatures(out string) ([]SysupdateComponent, error) {
	var comps []SysupdateComponent
	var cur *SysupdateComponent
	inTransfer := false
	fail := func(format string, a ...any) ([]SysupdateComponent, error) {
		return nil, fmt.Errorf("parse features: "+format, a...)
	}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			inTransfer = false
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			// Top-level token.
			inTransfer = false
			if strings.HasPrefix(line, "Component:") {
				comps = append(comps, SysupdateComponent{})
				cur = &comps[len(comps)-1]
				cur.Name = strings.TrimSpace(strings.TrimPrefix(line, "Component:"))
				continue
			}
			continue
		}
		if cur == nil {
			return fail("field %q before any Component", trimmed)
		}
		key, val, found := strings.Cut(strings.TrimPrefix(line, "\t"), ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "Transfer":
			inTransfer = true
		case "Title":
			if cur.Title == "" {
				cur.Title = val
			}
		case "Description":
			if cur.Description == "" {
				cur.Description = val
			}
		case "Path":
			cur.Paths = append(cur.Paths, val)
		case "Source":
			cur.Sources = append(cur.Sources, val)
		}
	}
	_ = inTransfer
	return comps, nil
}

// Features returns the configured sysupdate components, sorted by name.
func Features() ([]SysupdateComponent, error) {
	out, err := execSysupdateFeatures()
	if err != nil {
		return nil, fmt.Errorf("systemd-sysupdate features: %w", err)
	}
	comps, err := parseFeatures(out)
	if err != nil {
		return nil, err
	}
	sort.Slice(comps, func(i, j int) bool { return comps[i].Name < comps[j].Name })
	return comps, nil
}

// RebootOwed reports whether `systemd-sysupdate pending` indicates updates
// are staged but a reboot has not happened yet.
func RebootOwed() bool {
	out, err := execSysupdatePending()
	if err != nil {
		// The helper exits non-zero when there is nothing pending.
		return false
	}
	return strings.TrimSpace(out) != ""
}

// resolveLatestVersion determines the newest version for a sysupdate source
// URL. GitHub "latest/download" URLs redirect to a tag-pinned URL carrying the
// version on the path; the plain HTTP redirect is followed manually so no API
// access is needed.
func resolveLatestVersion(ctx context.Context, source string) (string, error) {
	if !strings.Contains(source, "releases/latest/download") {
		return "", nil
	}
	headURL := strings.Replace(source, "%v", "", 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, headURL, nil)
	if err != nil {
		return "", err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = nil
	client := &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 15_000_000_000,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", nil
	}
	return versionFromDownloadPath(loc), nil
}

// versionFromDownloadPath extracts the release tag from a GitHub download URL
// of the shape https://.../releases/download/<tag>/<asset>.
func versionFromDownloadPath(u string) string {
	marker := "/releases/download/"
	i := strings.Index(u, marker)
	if i < 0 {
		return ""
	}
	rest := u[i+len(marker):]
	j := strings.IndexByte(rest, '/')
	if j < 0 {
		return ""
	}
	return strings.TrimPrefix(rest[:j], "v")
}

// CompareVersions orders two version strings: numeric dotted segments first,
// remaining text compared case-insensitively. 0 means equal, -1 means a<b.
func CompareVersions(a, b string) int {
	return compareVersions(a, b)
}

func compareVersions(a, b string) int {
	as := splitVersion(a)
	bs := splitVersion(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, y := as[i], bs[i]
		nx, ex := numeric(x)
		ny, ey := numeric(y)
		switch {
		case ex && ey:
			if nx != ny {
				if nx < ny {
					return -1
				}
				return 1
			}
		case ex != ey:
			if ex {
				return 1 // numeric beats textual
			}
			return -1
		default:
			if c := compareText(x, y); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

// splitVersion splits on separators, keeping numeric leading-v stripping and
// allowing "v1.2" == "1.2".
func splitVersion(s string) []string {
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	return strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
}

func numeric(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

func compareText(a, b string) int {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	switch {
	case la < lb:
		return -1
	case la > lb:
		return 1
	}
	return 0
}
