#!/bin/sh
# Exercise the packaged sandbox in temporary system-manager units. Each probe
# gets a private network namespace, so binding port 443 cannot affect the host.
set -eu
cd -- "$(dirname -- "$0")/.."
python3 - "$@" <<'PY'
import configparser
import os
import platform
import subprocess
import sys

if sys.argv[1:]:
    sys.exit("usage: sudo -n bash packaging/systemd-test.sh (the system manager is required; --user is unsupported)")
if os.geteuid() != 0:
    sys.exit("This capability test requires the system manager: run sudo -n bash packaging/systemd-test.sh.")
if platform.machine() != "x86_64":
    sys.exit("The package and this openat2 syscall probe require Linux x86_64.")

unit = configparser.ConfigParser(interpolation=None)
unit.optionxform = str
unit.read("packaging/systemd/devbox-kvm.service")
service = unit["Service"]
properties = (
    "NoNewPrivileges", "CapabilityBoundingSet", "AmbientCapabilities",
    "ProtectSystem", "ProtectHome", "PrivateTmp", "ProtectKernelTunables",
    "ProtectKernelModules", "ProtectKernelLogs", "ProtectControlGroups",
    "RestrictSUIDSGID", "SystemCallFilter", "SystemCallErrorNumber",
    "SystemCallArchitectures",
)
command = ["systemd-run", "--wait", "--pipe", "--collect",
           "--property=User=nobody", "--property=PrivateNetwork=yes",
           "--property=WorkingDirectory=/", "--property=RuntimeMaxSec=30s"]
for name in properties:
    if name in service:
        command.append("--property=" + name + "=" + service[name])

# Match linuxVMCgroupSystem.OpenRoot and linuxVMCgroupDirectory.openControl.
# Pass the probe directly: PrivateTmp hides scripts stored in the caller's /tmp.
probe = r'''
import ctypes
import os
import socket
import sys

without_capabilities = sys.argv[1] == "without-capabilities"
expected_capabilities = 0 if without_capabilities else 1 << 10  # CAP_NET_BIND_SERVICE
with open("/proc/self/status") as status_file:
    status = dict(line.split(":", 1) for line in status_file if ":" in line)
assert os.geteuid() != 0, "probe must run as an unprivileged user"
assert int(status["NoNewPrivs"].strip()) == 1, "NoNewPrivileges must remain enabled"
for field in ("CapEff", "CapAmb"):
    actual = int(status[field].strip(), 16)
    assert actual == expected_capabilities, f"{field}: expected {expected_capabilities:#x}, got {actual:#x}"

class OpenHow(ctypes.Structure):
    _fields_ = [("flags", ctypes.c_uint64), ("mode", ctypes.c_uint64),
                ("resolve", ctypes.c_uint64)]

libc = ctypes.CDLL(None, use_errno=True)
libc.syscall.restype = ctypes.c_long
libc.syscall.argtypes = [ctypes.c_long, ctypes.c_int, ctypes.c_char_p,
                         ctypes.POINTER(OpenHow), ctypes.c_size_t]

def openat2(directory, path, flags, resolve):
    how = OpenHow(flags, 0, resolve)
    fd = libc.syscall(437, directory, path, ctypes.byref(how), ctypes.sizeof(how))
    if fd < 0:
        error = ctypes.get_errno()
        raise OSError(error, "openat2: " + os.strerror(error), os.fsdecode(path))
    return fd

no_links = 0x02 | 0x04  # RESOLVE_NO_MAGICLINKS | RESOLVE_NO_SYMLINKS
directory = openat2(-100, b"/sys/fs/cgroup",
                    os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC, no_links)
try:
    control = openat2(directory, b"cgroup.controllers",
                      os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW,
                      0x08 | no_links)  # RESOLVE_BENEATH
    try:
        os.read(control, 4096)
    finally:
        os.close(control)
finally:
    os.close(directory)

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
    try:
        listener.bind(("127.0.0.1", 443))
    except PermissionError:
        if without_capabilities:
            print("Port 443 correctly denied without capabilities.", flush=True)
            sys.exit(77)
        raise
    assert not without_capabilities, "port 443 unexpectedly allowed without capabilities"
print("Packaged sandbox permits secure cgroup opens and unprivileged port 443 binding with only CAP_NET_BIND_SERVICE.")
'''

result = subprocess.run(command + ["/usr/bin/python3", "-c", probe, "packaged-capabilities"], check=False)
if result.returncode:
    sys.exit(result.returncode)

# Override capabilities only for this collected transient unit. The packaged
# service retains its narrowly scoped capability and all sandbox properties.
control = subprocess.run(command + ["--property=CapabilityBoundingSet=", "--property=AmbientCapabilities=",
                         "/usr/bin/python3", "-c", probe, "without-capabilities"],
                         text=True, capture_output=True, check=False)
if control.returncode != 77 or "Port 443 correctly denied without capabilities." not in control.stdout:
    print(control.stdout, end="")
    print(control.stderr, end="", file=sys.stderr)
    sys.exit("No-capability control did not fail specifically with permission denied for port 443.")
print("No-capability control failed to bind port 443 as expected.")
PY
