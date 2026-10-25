#!/usr/bin/env bash
# Inspect built release artifacts without installing packages or starting services.
set -euo pipefail

python3 - "${1:-dist}" <<'PY'
import hashlib
import io
import json
from pathlib import Path
import stat
import subprocess
import sys
import tarfile
import tempfile


def require(condition, message):
    if not condition:
        raise ValueError(message)


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs).strip()


def check(dist):
    artifacts = json.loads((dist / "artifacts.json").read_text())
    packages = [a for a in artifacts if a["type"] == "Linux Package"]
    require(len(packages) == 2, "expected exactly one DEB and one RPM")
    require({Path(a["name"]).suffix for a in packages} == {".deb", ".rpm"},
            "release packages must be DEB and RPM")
    for artifact in artifacts:
        kind = artifact["type"]
        # GoReleaser uses the same display name for build-only Binary (4) and
        # UploadableBinary (2). The latter would release an unpackaged binary.
        internal = kind == "Metadata" or (kind == "Binary" and artifact["internal_type"] == 4)
        require(internal or kind in {"Linux Package", "Checksum"},
                f"unexpected release artifact: {kind} {artifact['name']}")
    checksums = [a for a in artifacts if a["type"] == "Checksum"]
    require(len(checksums) == 1, "expected one package checksum file")
    package_names = {a["name"] for a in packages}
    checksum_names = set()
    for line in (dist / checksums[0]["name"]).read_text().splitlines():
        digest, name = line.split(maxsplit=1)
        name = name.removeprefix("*")
        require(name in package_names and name not in checksum_names,
                f"unexpected checksum entry: {name}")
        checksum_names.add(name)
        actual = hashlib.sha256((dist / name).read_bytes()).hexdigest()
        require(actual == digest, f"checksum mismatch: {name}")
    require(checksum_names == package_names, "package missing from checksums")

    expected_files = {
        "/usr/bin/devbox-kvm": 0o755,
        "/usr/libexec/devbox-kvm/start-service": 0o755,
        "/usr/lib/systemd/system/devbox-kvm.service": 0o644,
        "/etc/devbox-kvm/devbox-kvm.env": 0o600,
    }
    config = "/etc/devbox-kvm/devbox-kvm.env"
    deb = dist / next(a["name"] for a in packages if a["name"].endswith(".deb"))
    rpm = dist / next(a["name"] for a in packages if a["name"].endswith(".rpm"))
    require(run("dpkg-deb", "--field", str(deb), "Package") == "devbox-kvm", "wrong DEB name")
    require(run("dpkg-deb", "--field", str(deb), "Architecture") == "amd64", "wrong DEB architecture")
    require(run("rpm", "--query", "--package", "--queryformat", "%{NAME}", str(rpm)) == "devbox-kvm",
            "wrong RPM name")
    require(run("rpm", "--query", "--package", "--queryformat", "%{ARCH}", str(rpm)) == "x86_64",
            "wrong RPM architecture")

    # Read archive ownership directly: unprivileged extraction substitutes the
    # caller's uid/gid and cannot prove that installation will use root:root.
    payload = subprocess.check_output(["dpkg-deb", "--fsys-tarfile", str(deb)])
    with tarfile.open(fileobj=io.BytesIO(payload)) as archive:
        members = {"/" + m.name.removeprefix("./").lstrip("/"): m for m in archive}
        for name, mode in expected_files.items():
            member = members.get(name)
            require(member is not None and member.isfile(), f"DEB missing regular file: {name}")
            require(member.mode == mode and member.uid == 0 and member.gid == 0,
                    f"incorrect DEB permissions/ownership: {name}")

    query = "[%{FILENAMES}\\t%{FILEMODES}\\t%{FILEUSERNAME}\\t%{FILEGROUPNAME}\\t%{FILEFLAGS}\\n]"
    rpm_files = {}
    for line in run("rpm", "--query", "--package", "--queryformat", query, str(rpm)).splitlines():
        name, mode, owner, group, flags = line.split("\t")
        rpm_files[name] = (int(mode), owner, group, int(flags))
    for name, expected_mode in expected_files.items():
        require(name in rpm_files, f"RPM missing regular file: {name}")
        mode, owner, group, _ = rpm_files[name]
        require(stat.S_ISREG(mode) and stat.S_IMODE(mode) == expected_mode and owner == group == "root",
                f"incorrect RPM permissions/ownership: {name}")
    # RPMFILE_CONFIG=1, RPMFILE_NOREPLACE=16 preserve administrator edits.
    require(rpm_files[config][3] & 17 == 17, "RPM configuration must be config(noreplace)")

    with tempfile.TemporaryDirectory(prefix="devbox-package-check-") as temporary:
        root = Path(temporary)
        control = root / "control"
        subprocess.run(["dpkg-deb", "--control", str(deb), str(control)], check=True)
        conffiles = (control / "conffiles").read_text().splitlines()
        require(config in conffiles, "DEB configuration must be a conffile")
        for name in ("preinst", "postinst", "prerm", "postrm"):
            script = control / name
            require(script.is_file() and script.stat().st_mode & 0o111, f"DEB missing executable {name}")
            subprocess.run(["sh", "-n", str(script)], check=True)
        for name in ("PREIN", "POSTIN", "PREUN", "POSTUN"):
            script = run("rpm", "--query", "--package", "--queryformat", "%{" + name + "}", str(rpm))
            require(script and script != "(none)", f"RPM missing {name} scriptlet")
            subprocess.run(["sh", "-n"], input=script, text=True, check=True)
        extracted = root / "package"
        subprocess.run(["dpkg-deb", "--extract", str(deb), str(extracted)], check=True)
        help_result = subprocess.run([str(extracted / "usr/bin/devbox-kvm"), "serve", "-help"],
                                     text=True, capture_output=True, timeout=15, check=True)
        require("Usage: devbox-kvm serve" in help_result.stdout + help_result.stderr,
                "packaged binary did not produce serve help")
    print("Package checks passed: DEB/RPM metadata, payloads, configuration preservation, scripts, checksums, and binary startup")


try:
    check(Path(sys.argv[1]).resolve())
except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
    sys.exit(f"Package checks failed: {error}")
PY
