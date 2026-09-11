package machine

import (
	"bufio"
	"os"
	"strings"
)

// osReleasePaths are checked in order; /etc/os-release wins on systems where
// it is a symlink to /usr/lib/os-release.
var osReleasePaths = []string{
	"/etc/os-release",
	"/usr/lib/os-release",
}

// ReadOSRelease parses an os-release(5) file into a key/value map,
// honoring unquoted, single-quoted, and double-quoted values.
func ReadOSRelease() map[string]string {
	out := map[string]string{}
	for _, p := range osReleasePaths {
		if out, ok := parseOSRelease(p); ok {
			return out
		}
	}
	return out
}

func parseOSRelease(path string) (map[string]string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return parseOSReleaseContent(string(b)), true
}

func parseOSReleaseContent(content string) map[string]string {
	vals := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
	}
	return vals
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
