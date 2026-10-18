package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetKernel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, version, failure string
		matching               bool
	}{
		{name: "matching_prebuilt", version: "5.14.3", matching: true},
		{name: "rebuild_patch_release", version: "5.14.3"},
		{name: "rebuild_initial_release", version: "6.5.0"},
		{name: "prebuilt_download_failure", version: "5.14.3", failure: "bzImage"},
		{name: "source_download_failure", version: "5.14.3", failure: "linux-5.14.3.tar.xz"},
		{name: "config_extraction_failure", version: "5.14.3", failure: "extract-ikconfig"},
		{name: "source_extraction_failure", version: "5.14.3", failure: "tar"},
		{name: "configuration_failure", version: "5.14.3", failure: "olddefconfig"},
		{name: "build_failure", version: "5.14.3", failure: "build"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			taskDir := newKernelFixture(t, tc.version, tc.matching)
			script, err := filepath.Abs("get_kernel.bash")
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), "bash", script)
			cmd.Dir = taskDir
			cmd.Env = append(os.Environ(), "KERNEL_FIXTURE_DIR="+taskDir, "KERNEL_FAILURE="+tc.failure,
				"PATH="+filepath.Join(taskDir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, runErr := cmd.CombinedOutput()
			calls := readKernelFixture(t, filepath.Join(taskDir, "calls"))
			if tc.failure != "" {
				if runErr == nil {
					t.Fatalf("failure at %s was ignored:\n%s", tc.failure, output)
				}
				if !strings.Contains(calls, "failure "+tc.failure+"\n") {
					t.Fatalf("did not reach the intended failure at %s:\n%s", tc.failure, output)
				}
				if strings.Contains(calls, "\nextract-vmlinux\n") {
					t.Fatalf("extracted a stale kernel after %s failed:\n%s", tc.failure, output)
				}

				return
			}
			if runErr != nil {
				t.Fatalf("get_kernel: %v\n%s", runErr, output)
			}
			want := "rebuilt-kernel\n"
			if tc.matching {
				want = "prebuilt-kernel\n"
				if strings.Contains(calls, "tar\n") || strings.Contains(calls, "make ") {
					t.Fatalf("matching configuration unexpectedly rebuilt the kernel:\n%s", calls)
				}
			} else {
				version := strings.TrimSuffix(tc.version, ".0")
				major, _, _ := strings.Cut(version, ".")
				url := "https://cdn.kernel.org/pub/linux/kernel/v" + major + ".x/linux-" + version + ".tar.xz"
				if !strings.Contains(calls, "curl "+url+" linux-"+version+".tar.xz\n") {
					t.Fatalf("source archive did not use the expected URL and filename:\n%s", calls)
				}
			}
			if got := readKernelFixture(t, filepath.Join(taskDir, "vmlinux")); got != want {
				t.Fatalf("extracted kernel = %q, want %q", got, want)
			}
		})
	}
}

func newKernelFixture(t *testing.T, version string, matching bool) string {
	t.Helper()
	taskDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(taskDir, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := "# Linux/x86 " + version + " Kernel Configuration\nCONFIG_TEST=y\n"
	prebuiltConfig := strings.ReplaceAll(config, "CONFIG_TEST=y", "# CONFIG_TEST is not set")
	if matching {
		prebuiltConfig = config
	}
	for name, content := range map[string]string{
		"linux.config": config, "prebuilt.config": prebuiltConfig,
		"calls": "", "bin/curl": kernelCurlFixture, "bin/tar": kernelTarFixture,
		"bin/make": kernelMakeFixture,
		"extract-ikconfig.fixture": `#!/bin/sh
printf '%s\n' extract-ikconfig >>"$KERNEL_FIXTURE_DIR/calls"
if [ "$KERNEL_FAILURE" = extract-ikconfig ]; then
    printf 'failure extract-ikconfig\n' >>"$KERNEL_FIXTURE_DIR/calls"
    exit 1
fi
cat "$KERNEL_FIXTURE_DIR/prebuilt.config"
`,
		"extract-vmlinux.fixture": `#!/bin/sh
printf '%s\n' extract-vmlinux >>"$KERNEL_FIXTURE_DIR/calls"
cat "$1"
`,
	} {
		mode := os.FileMode(0o600)
		if strings.HasPrefix(name, "bin/") || strings.HasSuffix(name, ".fixture") {
			mode = 0o700
		}
		if err := os.WriteFile(filepath.Join(taskDir, name), []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}

	return taskDir
}

func readKernelFixture(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

const kernelCurlFixture = `#!/bin/bash
set -euo pipefail
output=
url=
remote_name=false
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o|--output)
            [ -z "$output" ] || exit 2
            output=$2
            shift 2
            ;;
        -O|--remote-name) remote_name=true; shift ;;
        --retry|-C|--continue-at) shift 2 ;;
        -*) shift ;;
        *) url=$1; shift ;;
    esac
done
[ -n "$url" ]
name=${url##*/}
if [ "$remote_name" = true ]; then
    [ -z "$output" ] || exit 2
    output=$name
fi
[ -n "$output" ]
printf 'curl %s %s\n' "$url" "$output" >>"$KERNEL_FIXTURE_DIR/calls"
if [ "$KERNEL_FAILURE" = "$name" ] && [[ "$name" != extract-* ]]; then
    printf 'failure %s\n' "$name" >>"$KERNEL_FIXTURE_DIR/calls"
    exit 22
fi
case "$name" in
    extract-ikconfig|extract-vmlinux)
        cp "$KERNEL_FIXTURE_DIR/$name.fixture" "$output"
        ;;
    linux-*.tar.xz)
        [ "$output" = "$name" ] || exit 2
        printf 'source archive\n' >"$output"
        ;;
    *)
        printf 'prebuilt-kernel\n' >"$output"
        ;;
esac
`

const kernelTarFixture = `#!/bin/bash
set -euo pipefail
printf '%s\n' tar >>"$KERNEL_FIXTURE_DIR/calls"
if [ "$KERNEL_FAILURE" = tar ]; then
    printf 'failure tar\n' >>"$KERNEL_FIXTURE_DIR/calls"
    exit 1
fi
archive=
directory=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -Jxf|Jxf) archive=$2; shift 2 ;;
        -C) directory=$2; shift 2 ;;
        *) shift ;;
    esac
done
[ -n "$archive" ]
[ -f "$archive" ]
[ -n "$directory" ]
[ -d "$directory" ]
mkdir -p "$directory/arch/x86/boot"
`

const kernelMakeFixture = `#!/bin/bash
set -euo pipefail
directory=
stage=build
while [ "$#" -gt 0 ]; do
    case "$1" in
        -C) directory=$2; shift 2 ;;
        olddefconfig) stage=olddefconfig; shift ;;
        *) shift ;;
    esac
done
printf 'make %s\n' "$stage" >>"$KERNEL_FIXTURE_DIR/calls"
if [ "$KERNEL_FAILURE" = "$stage" ]; then
    printf 'failure %s\n' "$stage" >>"$KERNEL_FIXTURE_DIR/calls"
    exit 1
fi
cmp "$directory/.config" "$KERNEL_FIXTURE_DIR/linux.config"
if [ "$stage" = build ]; then
    printf 'rebuilt-kernel\n' >"$directory/arch/x86/boot/bzImage"
fi
`
