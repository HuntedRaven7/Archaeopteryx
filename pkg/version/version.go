// Package version holds build-time version information for apxd and apxctl.
package version

import (
	"fmt"
	"runtime"
)

// These are meant to be overridden at build time via -ldflags.
var (
	Version   = "devel"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// Info is the build information payload reported by both binaries.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Current returns the build information for the running process.
func Current() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// String renders a compact single-line version string.
func (i Info) String() string {
	return fmt.Sprintf("%s (%s)", i.Version, i.Commit)
}
