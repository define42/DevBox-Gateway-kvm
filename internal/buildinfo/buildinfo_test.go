package buildinfo

import (
	"encoding/json"
	"runtime/debug"
	"testing"
)

func TestResolveReleaseValues(t *testing.T) {
	t.Parallel()

	embedded := &debug.BuildInfo{
		GoVersion: "go-fallback",
		Main:      debug.Module{Version: "v0.0.0-fallback"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "fallback-commit"},
			{Key: "vcs.modified", Value: "false"},
		},
	}
	got := resolve("v1.2.3", "release-commit", "2026-10-29T12:00:00Z", "go-runtime", embedded)

	if got.Version != "v1.2.3" {
		t.Fatalf("Version = %q, want linker value", got.Version)
	}
	if got.Commit != "release-commit" {
		t.Fatalf("Commit = %q, want linker value", got.Commit)
	}
	if got.BuildDate != "2026-10-29T12:00:00Z" {
		t.Fatalf("BuildDate = %q, want linker value", got.BuildDate)
	}
	if got.GoVersion != "go-fallback" {
		t.Fatalf("GoVersion = %q, want embedded value", got.GoVersion)
	}
	if got.Modified == nil || *got.Modified {
		t.Fatalf("Modified = %v, want known false", got.Modified)
	}
}

func TestResolveDevelopmentFallback(t *testing.T) {
	t.Parallel()

	embedded := &debug.BuildInfo{
		GoVersion: "go1.26.0",
		Main:      debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "development-commit"},
			{Key: "vcs.time", Value: "2026-10-28T09:30:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	got := resolve("", "", "", "go-runtime", embedded)

	if got.Version != developmentVersion {
		t.Fatalf("Version = %q, want normalized development version", got.Version)
	}
	if got.Commit != "development-commit" {
		t.Fatalf("Commit = %q, want embedded revision", got.Commit)
	}
	if got.BuildDate != "" {
		t.Fatalf("BuildDate = %q, want empty rather than the VCS commit time", got.BuildDate)
	}
	if got.GoVersion != "go1.26.0" {
		t.Fatalf("GoVersion = %q, want embedded toolchain version", got.GoVersion)
	}
	if got.Modified == nil || !*got.Modified {
		t.Fatalf("Modified = %v, want known true", got.Modified)
	}
}

func TestResolveUnknownBuildInfo(t *testing.T) {
	t.Parallel()

	got := resolve("", "", "", "go-runtime", nil)
	if got != (Info{Version: developmentVersion, GoVersion: "go-runtime"}) {
		t.Fatalf("resolve() = %+v, want development and runtime Go versions", got)
	}

	got = resolve("", "", "", "go-runtime", &debug.BuildInfo{
		Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "unknown"}},
	})
	if got.Modified != nil {
		t.Fatalf("Modified = %v, want unknown value omitted", got.Modified)
	}
}

func TestInfoJSONAllowlist(t *testing.T) {
	t.Parallel()

	modified := false
	info := Info{
		Version:   "v1.2.3",
		Commit:    "abcdef",
		BuildDate: "2026-10-29T12:00:00Z",
		GoVersion: "go1.26.0",
		Modified:  &modified,
	}
	got, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"version":"v1.2.3","commit":"abcdef",` +
		`"buildDate":"2026-10-29T12:00:00Z","goVersion":"go1.26.0","modified":false}`
	if string(got) != want {
		t.Fatalf("json.Marshal() = %s, want %s", got, want)
	}

	got, err = json.Marshal(resolve("", "", "", "go1.26.0", nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"version":"devel","goVersion":"go1.26.0"}` {
		t.Fatalf("json.Marshal() with unknown optional values = %s", got)
	}
}
