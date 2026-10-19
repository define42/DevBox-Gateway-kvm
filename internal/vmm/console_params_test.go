package vmm

import "testing"

func TestConsoleBootParams(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		params string
		want   string
	}{
		{name: "empty", want: "console=tty0"},
		{
			name: "serial and early consoles",
			params: "console=tty0 console=ttyS0,115200n8 earlyprintk=serial " +
				"earlycon=uart8250,io,0x3f8 root=/dev/vda1 rdinit=/init",
			want: "console=tty0 root=/dev/vda1 rdinit=/init",
		},
		{
			name:   "all kernel consoles",
			params: "console=hvc0 console=tty1 earlycon earlyprintk=vga root=LABEL=desktop",
			want:   "console=tty0 root=LABEL=desktop",
		},
		{
			name: "quoted arguments",
			params: `root="LABEL=Desktop Disk" "console=ttyS0,115200n8" console="tty0" ` +
				`earlyprintk="serial,ttyS0,115200" dyndbg="file smpboot.c +p"`,
			want: `console=tty0 root="LABEL=Desktop Disk" dyndbg="file smpboot.c +p"`,
		},
		{
			name:   "unrelated names and values",
			params: `myconsole=ttyS0 consoleblank=0 foo="console=ttyS0 earlycon" rdinit=/init`,
			want:   `console=tty0 myconsole=ttyS0 consoleblank=0 foo="console=ttyS0 earlycon" rdinit=/init`,
		},
		{
			name:   "init arguments untouched",
			params: "console=ttyS0 root=/dev/vda1\t--  console=ttyS1\tearlycon=uart8250 foo=\"a b\"\n",
			want:   "console=tty0 root=/dev/vda1\t--  console=ttyS1\tearlycon=uart8250 foo=\"a b\"\n",
		},
		{
			name:   "only init arguments",
			params: "-- console=ttyS0",
			want:   "console=tty0 -- console=ttyS0",
		},
		{
			name:   "quoted init delimiter",
			params: `"--" console=ttyS0 earlyprintk=serial`,
			want:   `console=tty0 "--" console=ttyS0 earlyprintk=serial`,
		},
		{
			name:   "preserve remaining whitespace",
			params: "  root=/dev/vda1\tconsole=ttyS0\nrdinit=/init  ",
			want:   "console=tty0  root=/dev/vda1\nrdinit=/init  ",
		},
		{
			name:   "unclosed unrelated quote",
			params: `root="console=ttyS0 earlycon`,
			want:   `console=tty0 root="console=ttyS0 earlycon`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := New(gatewayRDPConfig(t))
			if got := v.consoleBootParams(tc.params); got != tc.want {
				t.Fatalf("consoleBootParams(%q) = %q, want %q", tc.params, got, tc.want)
			}
			if got := v.consoleBootParams(tc.want); got != tc.want {
				t.Fatalf("repeated console policy changed arguments: %q", got)
			}
		})
	}
}

func TestConsoleBootParamsWithoutRDP(t *testing.T) {
	t.Parallel()

	const params = " root=\"LABEL=Desktop Disk\"\tconsole=ttyS0 earlyprintk=serial -- earlycon\n"
	if got := New(Config{}).consoleBootParams(params); got != params {
		t.Fatalf("headless arguments changed: %q", got)
	}
}
