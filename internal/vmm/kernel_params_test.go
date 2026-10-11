package vmm

import "testing"

func TestMergeKernelParamsPreservesEmbeddedArguments(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		cmdline  string
		defaults []string
		want     string
	}{
		{
			name:     "quoted whitespace remains exact",
			cmdline:  "\tcaption=\"two  words\"\nrdinit=/init  ",
			defaults: []string{"noacpi"},
			want:     "noacpi \tcaption=\"two  words\"\nrdinit=/init  ",
		},
		{
			name:     "all defaults overridden",
			cmdline:  "noacpi\tconsole=ttyS0,115200n8  ",
			defaults: []string{"noacpi", "console=ttyS0"},
			want:     "noacpi\tconsole=ttyS0,115200n8  ",
		},
		{
			name:    "no defaults",
			cmdline: " caption=\"a  b\" ",
			want:    " caption=\"a  b\" ",
		},
		{
			name:     "empty embedded arguments",
			defaults: []string{"noacpi", "console=ttyS0"},
			want:     "noacpi console=ttyS0",
		},
		{
			name:     "parameter names inside quoted value do not override",
			cmdline:  `caption="hello noacpi console=ttyS0 world"`,
			defaults: []string{"noacpi", "console=ttyS0"},
			want:     `noacpi console=ttyS0 caption="hello noacpi console=ttyS0 world"`,
		},
		{
			name:     "quoted console value",
			cmdline:  `console="ttyS0,115200n8"`,
			defaults: []string{"console=tty0", "console=ttyS0"},
			want:     `console=tty0 console="ttyS0,115200n8"`,
		},
		{
			name:     "quoted console device without options",
			cmdline:  `console="ttyS0"`,
			defaults: []string{"console=ttyS0"},
			want:     `console="ttyS0"`,
		},
		{
			name:     "entire parameter quoted",
			cmdline:  `"console=ttyS0,115200n8" "noacpi"`,
			defaults: []string{"console=ttyS0", "noacpi"},
			want:     `"console=ttyS0,115200n8" "noacpi"`,
		},
		{
			name:     "init arguments cannot override kernel defaults",
			cmdline:  `rdinit=/init -- noacpi console=ttyS0`,
			defaults: []string{"noacpi", "console=ttyS0"},
			want:     `noacpi console=ttyS0 rdinit=/init -- noacpi console=ttyS0`,
		},
		{
			name:     "quoted init separator",
			cmdline:  `"--" noacpi`,
			defaults: []string{"noacpi"},
			want:     `noacpi "--" noacpi`,
		},
		{
			name:     "separator inside quoted value",
			cmdline:  `caption="a -- b" noacpi`,
			defaults: []string{"noacpi"},
			want:     `caption="a -- b" noacpi`,
		},
		{
			name:     "single quotes do not group",
			cmdline:  `caption='a noacpi b'`,
			defaults: []string{"noacpi"},
			want:     `caption='a noacpi b'`,
		},
		{
			name:     "backslash does not escape quote",
			cmdline:  `caption="a\" noacpi`,
			defaults: []string{"noacpi"},
			want:     `caption="a\" noacpi`,
		},
		{
			name:     "backslash does not escape whitespace",
			cmdline:  `caption=a\ noacpi`,
			defaults: []string{"noacpi"},
			want:     `caption=a\ noacpi`,
		},
		{
			name:     "unterminated quote groups to end",
			cmdline:  `caption="a noacpi`,
			defaults: []string{"noacpi"},
			want:     `noacpi caption="a noacpi`,
		},
		{
			name:     "kernel Latin-1 whitespace",
			cmdline:  "caption=a\xa0noacpi",
			defaults: []string{"noacpi"},
			want:     "caption=a\xa0noacpi",
		},
		{
			name:     "empty quoted argument",
			cmdline:  `"" noacpi`,
			defaults: []string{"noacpi"},
			want:     `"" noacpi`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := mergeKernelParams(tc.cmdline, tc.defaults); got != tc.want {
				t.Fatalf("mergeKernelParams() = %q, want %q", got, tc.want)
			}
		})
	}
}
