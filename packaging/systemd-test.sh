#!/bin/sh
# Exercise the packaged systemd sandbox without installing the package or
# writing host cgroups. Use sudo for the system manager, or pass --user.
set -eu
cd -- "$(dirname -- "$0")/.."
python3 - "$@" <<'PY'
import configparser
import platform
import subprocess
import sys

if sys.argv[1:] not in ([], ["--user"]):
    sys.exit("usage: packaging/systemd-test.sh [--user]")
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
command = ["systemd-run", *sys.argv[1:], "--wait", "--pipe", "--collect",
           "--property=WorkingDirectory=/", "--property=RuntimeMaxSec=30s"]
for name in properties:
    if name in service:
        command.append("--property=" + name + "=" + service[name])

# Match linuxVMCgroupSystem.OpenRoot and linuxVMCgroupDirectory.openControl.
# Pass the probe directly: PrivateTmp hides scripts stored in the caller's /tmp.
probe = r'''
import ctypes
import os

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
print("Packaged systemd sandbox permits secure cgroup opens.")
'''
command.extend(["/usr/bin/python3", "-c", probe])
sys.exit(subprocess.run(command, check=False).returncode)
PY
