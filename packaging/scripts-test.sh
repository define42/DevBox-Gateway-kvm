#!/bin/sh
# Exercise package lifecycle decisions with mocked system tools. No accounts,
# installed files, running services or host cgroups are changed.
set -eu
cd -- "$(dirname -- "$0")/.."
python3 - <<'PY'
import os
import shutil
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
        [ "${PACKAGE_TEST_FAIL:-}" != "id$1" ] || exit 1
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
    stat)
        [ "${PACKAGE_TEST_FAIL:-}" != stat ] || exit 1
        printf '%s\\n' "${PACKAGE_TEST_FILESYSTEM:-cgroup2fs}"
        ;;
    mkdir)
        [ "${PACKAGE_TEST_FAIL:-}" != mkdir ] || exit 1
        exec /usr/bin/mkdir "$@"
        ;;
    chmod)
        [ "${PACKAGE_TEST_FAIL:-}" != "chmod$1" ] || exit 1
        exec /usr/bin/chmod "$@"
        ;;
    devbox-kvm)
        printf 'VM_CGROUP_ROOT <%s>\\n' "$VM_CGROUP_ROOT" >> "$PACKAGE_TEST_LOG"
        exit "${PACKAGE_TEST_GATEWAY_EXIT:-0}"
        ;;
    groupadd|useradd|install|configure-firewall) exit 0 ;;
    *) exit 99 ;;
esac
'''
    for name in ("getent", "id", "groupadd", "useradd", "install", "systemctl",
                 "configure-firewall", "stat", "mkdir", "chmod", "devbox-kvm"):
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
    service = directory / "devbox-kvm.service"
    workers = service / "workers"
    launcher = directory / "start-service"
    launcher.write_text((root / "packaging/start-service.sh").read_text().replace(
        "/proc/self/cgroup", str(cgroup)).replace(
        "/sys/fs/cgroup/system.slice/devbox-kvm.service", str(service)).replace(
        "/usr/bin/devbox-kvm", str(mock / "devbox-kvm")))

    def prepare_launcher():
        # Ordinary temporary files model control-file I/O without touching cgroups.
        if service.exists():
            shutil.rmtree(service)
        workers.mkdir(parents=True)
        cgroup.write_text("0::/system.slice/devbox-kvm.service/supervisor\n")
        (service / "cgroup.subtree_control").write_text("")
        for control in ("cgroup.kill", "cgroup.procs", "cgroup.threads", "cgroup.subtree_control"):
            (workers / control).write_text("")
        (workers / "cgroup.events").write_text("populated 0\n")

    def launch(values=None):
        logfile.write_text("")
        settings = dict(environment, LDAP_URL="ldaps://directory.example.com", AUTH_FILE="",
                        VM_CGROUP_ROOT="/must-not-be-used", PACKAGE_TEST_FAIL="",
                        PACKAGE_TEST_UID="987", PACKAGE_TEST_USER="devbox-kvm",
                        PACKAGE_TEST_FILESYSTEM="cgroup2fs", PACKAGE_TEST_GATEWAY_EXIT="0")
        settings.update(values or {})
        return subprocess.run(["/bin/sh", str(launcher)], env=settings,
                              capture_output=True, text=True)

    def deny_write(path):
        # A directory deterministically fails redirection, including when tests run as root.
        path.unlink()
        path.mkdir()

    def symlink_workers():
        shutil.rmtree(workers)
        workers.symlink_to(directory)

    cases = (
        ({"PACKAGE_TEST_UID": "0"}, None, "start the gateway with systemctl"),
        ({"PACKAGE_TEST_USER": "somebody"}, None, "start the gateway with systemctl"),
        ({"PACKAGE_TEST_FAIL": "id-u"}, None, "cannot determine the service UID"),
        ({"PACKAGE_TEST_FAIL": "id-un"}, None, "cannot resolve the service user"),
        ({"LDAP_URL": ""}, None, "configure LDAP_URL and LDAP_BASE_DN"),
        ({}, lambda: cgroup.write_text("0::/unrelated.service\n"), "requires systemd 254+"),
        ({}, cgroup.unlink, "cannot read the service cgroup membership"),
        ({"PACKAGE_TEST_FAIL": "stat"}, None, "cannot inspect the service cgroup"),
        ({"PACKAGE_TEST_FILESYSTEM": "tmpfs"}, None, "unified cgroup v2 hierarchy"),
        ({}, symlink_workers, "must not be a symbolic link"),
        ({}, lambda: (workers / "cgroup.events").write_text("populated 1\n"),
         "cannot confirm the VM subtree is empty"),
        ({}, lambda: (workers / "cgroup.events").unlink(), "cannot confirm the VM subtree is empty"),
        ({}, lambda: deny_write(service / "cgroup.subtree_control"), "cannot enable memory and pids"),
        ({"PACKAGE_TEST_FAIL": "mkdir"}, None, "cannot create the VM cgroup"),
        ({"PACKAGE_TEST_FAIL": "chmod0700"}, None, "cannot restrict access to the VM cgroup"),
        ({}, lambda: deny_write(workers / "cgroup.subtree_control"), "cannot delegate memory and pids"),
        ({}, lambda: (workers / "cgroup.kill").unlink(), "writable cgroup.kill is required"),
        ({"PACKAGE_TEST_FAIL": "chmod0600"}, None, "cannot restrict access to VM cgroup control"),
    )
    for values, change, message in cases:
        prepare_launcher()
        if change:
            change()
        result = launch(values)
        assert result.returncode == 78 and message in result.stderr, (message, result.returncode, result.stderr)
        assert "devbox-kvm <serve>" not in logfile.read_text(), logfile.read_text()

    for gateway_exit in (0, 1, 78):
        prepare_launcher()
        result = launch({"PACKAGE_TEST_GATEWAY_EXIT": str(gateway_exit)})
        assert result.returncode == gateway_exit, (gateway_exit, result.returncode, result.stderr)
        calls = logfile.read_text()
        assert "devbox-kvm <serve>" in calls, calls
        assert f"VM_CGROUP_ROOT <{workers}>" in calls, calls
        assert (service / "cgroup.subtree_control").read_text() == "+memory +pids\n"
        assert (workers / "cgroup.subtree_control").read_text() == "+memory +pids\n"
        assert workers.stat().st_mode & 0o777 == 0o700
        for control in ("cgroup.procs", "cgroup.threads", "cgroup.subtree_control"):
            assert (workers / control).stat().st_mode & 0o777 == 0o600

print("Package lifecycle and launcher failure checks passed.")
PY
