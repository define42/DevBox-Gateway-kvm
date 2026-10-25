#!/bin/sh
# Exercise package lifecycle decisions with mocked system tools. No accounts,
# installed files, running services or host cgroups are changed.
set -eu
cd -- "$(dirname -- "$0")/.."
python3 - <<'PY'
import os
from pathlib import Path
import subprocess
import tempfile

root = Path.cwd()
with tempfile.TemporaryDirectory(prefix="devbox-package-scripts-") as temporary:
    directory = Path(temporary)
    mock = directory / "bin"
    mock.mkdir()
    running_systemd = directory / "systemd"
    running_systemd.mkdir()
    logfile = directory / "calls.log"
    dispatcher = '''#!/bin/sh
set -eu
name=${0##*/}
printf '%s' "$name" >> "$PACKAGE_TEST_LOG"
for arg in "$@"; do printf ' <%s>' "$arg" >> "$PACKAGE_TEST_LOG"; done
printf '\\n' >> "$PACKAGE_TEST_LOG"
case "$name" in
    getent) [ "${PACKAGE_TEST_EXISTING:-0}" = 1 ] ;;
    id)
        case "$1" in
            -u) printf '%s\\n' "${PACKAGE_TEST_UID:-987}" ;;
            -un) printf '%s\\n' "${PACKAGE_TEST_USER:-devbox-kvm}" ;;
            *) exit 99 ;;
        esac
        ;;
    systemctl)
        if [ "$1" = is-active ]; then
            [ "${PACKAGE_TEST_ACTIVE:-0}" = 1 ]
        else
            exit 0
        fi
        ;;
    groupadd|useradd|install|configure-firewall) exit 0 ;;
    *) exit 99 ;;
esac
'''
    for name in ("getent", "id", "groupadd", "useradd", "install", "systemctl", "configure-firewall"):
        command = mock / name
        command.write_text(dispatcher)
        command.chmod(0o755)
    environment = dict(os.environ, PATH=str(mock) + ":" + os.environ["PATH"],
                       PACKAGE_TEST_LOG=str(logfile))
    scripts = directory / "scripts"
    scripts.mkdir()
    for source in (root / "packaging/scripts").glob("*.sh"):
        # Model a booted systemd host even in an offline CI build environment.
        (scripts / source.name).write_text(source.read_text().replace(
            "/run/systemd/system", str(running_systemd)).replace(
            "/usr/libexec/devbox-kvm/configure-firewall", str(mock / "configure-firewall")))

    def run(script, argument="", existing=False, uid="987", active=False, expected=0):
        logfile.write_text("")
        result = subprocess.run(["/bin/sh", str(scripts / script), argument],
                                env=dict(environment,
                                         PACKAGE_TEST_EXISTING=str(int(existing)),
                                         PACKAGE_TEST_ACTIVE=str(int(active)),
                                         PACKAGE_TEST_UID=uid),
                                capture_output=True, text=True)
        assert result.returncode == expected, (script, argument, result.stderr)
        return logfile.read_text()

    for argument in ("install", "upgrade", "1", "2"):
        calls = run("preinstall.sh", argument)
        assert calls == "systemctl <is-active> <--quiet> <devbox-kvm.service>\n", calls
        calls = run("preinstall.sh", argument, active=True)
        assert "systemctl <stop> <devbox-kvm.service>" in calls, calls
        assert all("systemctl <" + action + ">" not in calls
                   for action in ("start", "restart", "enable", "disable")), calls

    for argument in ("configure", "1"):
        calls = run("postinstall.sh", argument)
        assert "groupadd <--system> <devbox-kvm>" in calls, calls
        assert "groupadd <--system> <kvm>" in calls, calls
        assert "useradd <--system>" in calls, calls
        assert "install <-d> <-o> <devbox-kvm> <-g> <devbox-kvm> <-m> <0700> </var/lib/devbox-kvm>" in calls, calls
        assert "systemctl <daemon-reload>" in calls, calls
        assert all("systemctl <" + action + ">" not in calls
                   for action in ("start", "restart", "enable")), calls
    for argument in ("configure", "2"):
        calls = run("postinstall.sh", argument, existing=True)
        assert "useradd" not in calls and "groupadd" not in calls, calls
        assert all("systemctl <" + action + ">" not in calls
                   for action in ("start", "restart", "enable")), calls
    assert run("postinstall.sh", "abort-upgrade") == ""
    calls = run("postinstall.sh", "configure", existing=True, uid="0", expected=1)
    assert "install " not in calls and "systemctl " not in calls, calls

    for argument in ("upgrade", "failed-upgrade", "1", "2"):
        assert run("preremove.sh", argument) == ""
    for argument in ("remove", "0"):
        calls = run("preremove.sh", argument)
        assert "systemctl <stop> <devbox-kvm.service>" in calls, calls
        assert "systemctl <disable> <devbox-kvm.service>" in calls, calls
        assert "configure-firewall <--remove>" in calls, calls
    assert run("postremove.sh", "purge") == "systemctl <daemon-reload>\n"
    running_systemd.rmdir()
    assert run("preinstall.sh", "upgrade", active=True) == ""
    assert "systemctl " not in run("postinstall.sh", "configure", existing=True)
    assert run("preremove.sh", "remove") == "configure-firewall <--remove>\n"
    assert run("postremove.sh", "purge") == ""

    cgroup = directory / "cgroup"
    cgroup.write_text("0::/unrelated.service\n")
    launcher = directory / "start-service"
    launcher.write_text((root / "packaging/start-service.sh").read_text().replace(
        "/proc/self/cgroup", str(cgroup)))
    cases = (
        ({"PACKAGE_TEST_UID": "0"}, "start the gateway with systemctl"),
        ({"PACKAGE_TEST_USER": "somebody"}, "start the gateway with systemctl"),
        ({}, "configure LDAP_URL and LDAP_BASE_DN"),
        ({"LDAP_URL": "ldaps://directory.example.com"}, "requires systemd 254+"),
    )
    for values, message in cases:
        result = subprocess.run(["/bin/sh", str(launcher)],
                                env=dict(environment, LDAP_URL="", AUTH_FILE="", **values)
                                if "LDAP_URL" not in values else
                                dict(environment, AUTH_FILE="", **values),
                                capture_output=True, text=True)
        assert result.returncode == 1 and message in result.stderr, result.stderr

print("Package lifecycle and launcher failure checks passed.")
PY
