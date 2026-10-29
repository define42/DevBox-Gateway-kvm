// Package buildinfo reports the provenance embedded in the running binary.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strconv"
)

const developmentVersion = "devel"

// These values are set for release binaries by the GoReleaser linker flags.
//
//nolint:gochecknoglobals // GoReleaser's -X linker flags require addressable package variables.
var (
	releaseVersion   string
	releaseCommit    string
	releaseBuildDate string
)

// Info is the public, JSON-safe subset of the binary's build metadata.
// Modified is nil when Go did not record the source tree's VCS state.
type Info struct {
	Version   string `json:"version,omitempty"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"buildDate,omitempty"`
	GoVersion string `json:"goVersion"`
	Modified  *bool  `json:"modified,omitempty"`
}

// Current returns metadata for the running binary. Linker-injected release
// values take precedence over the module and VCS metadata recorded by Go.
func Current() Info {
	var embedded *debug.BuildInfo
	if info, ok := debug.ReadBuildInfo(); ok {
		embedded = info
	}

	return resolve(releaseVersion, releaseCommit, releaseBuildDate, runtime.Version(), embedded)
}

func resolve(version, commit, buildDate, goVersion string, embedded *debug.BuildInfo) Info {
	info := Info{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
		GoVersion: goVersion,
	}
	if embedded != nil && info.Version == "" {
		info.Version = embedded.Main.Version
	}
	if info.Version == "" || info.Version == "(devel)" {
		info.Version = developmentVersion
	}
	if embedded == nil {
		return info
	}
	if embedded.GoVersion != "" {
		info.GoVersion = embedded.GoVersion
	}
	for _, setting := range embedded.Settings {
		switch setting.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = setting.Value
			}
		case "vcs.modified":
			modified, err := strconv.ParseBool(setting.Value)
			if err == nil {
				info.Modified = &modified
			}
		}
	}

	return info
}
