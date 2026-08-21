// Package version reports the build metadata of the fab binary. The values are
// injected at link time by the Makefile.
package version

import (
	"fmt"
	"runtime"
)

var (
	gitVersion   = "v0.0.0-dev"
	gitCommit    = "unknown"
	gitTreeState = "unknown"
	buildDate    = "unknown"
)

// Info holds the build metadata of this binary.
type Info struct {
	// GitVersion is the semantic version of the build.
	GitVersion string `json:"gitVersion"`
	// GitCommit is the commit the binary was built from.
	GitCommit string `json:"gitCommit"`
	// GitTreeState is "clean" or "dirty" at build time.
	GitTreeState string `json:"gitTreeState"`
	// BuildDate is the RFC 3339 build timestamp.
	BuildDate string `json:"buildDate"`
	// GoVersion is the Go runtime version.
	GoVersion string `json:"goVersion"`
	// Compiler is the Go compiler used.
	Compiler string `json:"compiler"`
	// Platform is the GOOS/GOARCH pair.
	Platform string `json:"platform"`
}

// Get returns the build metadata of this binary.
func Get() Info {
	return Info{
		GitVersion:   gitVersion,
		GitCommit:    gitCommit,
		GitTreeState: gitTreeState,
		BuildDate:    buildDate,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// String returns the one-line form used by `fab version --short`.
func (i Info) String() string {
	return i.GitVersion
}
