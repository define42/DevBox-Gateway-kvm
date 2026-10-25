#!/bin/sh
# All firewall commands are mocked; only private temporary files are changed.
set -eu
cd -- "$(dirname -- "$0")/.."
python3 - <<'PY'
import json
import os
from pathlib import Path
import subprocess
import tempfile

source = Path("packaging/configure-firewall.sh").read_text()
with tempfile.TemporaryDirectory(prefix="devbox-firewall-test-") as temporary:
    root = Path(temporary)
    commands = root / "bin"
    commands.mkdir()
    state_dir = root / "owned"
    profiles = root / "profiles"
    profiles.mkdir()
    state_file = root / "firewalls.json"
    calls_file = root / "calls.jsonl"
    dispatcher = r'''#!/usr/bin/python3
import json
import os
from pathlib import Path
import subprocess
import sys

name = Path(sys.argv[0]).name
args = sys.argv[1:]
state_path = Path(os.environ["FIREWALL_TEST_STATE"])
state = json.loads(state_path.read_text())
with open(os.environ["FIREWALL_TEST_CALLS"], "a") as calls:
    calls.write(json.dumps([name, *args]) + "\n")
def finish(code=0):
    state_path.write_text(json.dumps(state))
    sys.exit(code)
if name == "id":
    print("0")
    finish()
if name == "install":
    filtered = []
    while args:
        arg = args.pop(0)
        if arg in ("-o", "-g"):
            args.pop(0)
        else:
            filtered.append(arg)
    finish(subprocess.run(["/usr/bin/install", *filtered]).returncode)
if state.get("fail") and state["fail"] in " ".join([name, *args]):
    print("injected firewall command failure", file=sys.stderr)
    finish(2)
if name in ("firewall-cmd", "firewall-offline-cmd"):
    if args == ["--state"]:
        print("running" if state["firewalld"] else "not running")
        finish(0 if state["firewalld"] else 252)
    if args == ["--get-default-zone"]:
        print(state["default_zone"])
        finish()
    if args == ["--get-active-zones"]:
        for zone in state["zones"]:
            print(zone + "\n\tinterfaces: test0")
        finish()
    plane = "permanent" if "--permanent" in args or name == "firewall-offline-cmd" else "runtime"
    zone = next(a.split("=", 1)[1] for a in args if a.startswith("--zone="))
    if zone in state.get("deleted_zones", []):
        finish(112)  # firewalld INVALID_ZONE
    options = [a for a in args if "-port=" in a]
    assert len(options) == 1, args
    operation, port = options[0].removeprefix("--").split("-port=")
    assert port.endswith("/tcp"), port
    key = "/".join([plane, zone, port[:-4]])
    if operation == "query":
        print("yes" if key in state["ports"] else "no")
        finish(0 if key in state["ports"] else 1)
    if operation == "add":
        if key not in state["ports"]:
            state["ports"].append(key)
        finish()
    if operation == "remove":
        if key in state["ports"]:
            state["ports"].remove(key)
        finish()
    raise AssertionError(args)
if name == "ufw":
    if args == ["status"]:
        print("Status: active" if state["ufw"] else "Status: inactive")
        finish()
    if args[:2] == ["app", "info"]:
        profile = args[2]
        profile_file = Path(os.environ["FIREWALL_TEST_PROFILES"]) / ("devbox-kvm-" + profile.removeprefix("DevBox-KVM-"))
        finish(0 if profile_file.exists() or profile in state.get("foreign_profiles", []) else 1)
    if args[:2] == ["--force", "delete"]:
        assert args[2] == "allow", args
        profile = args[3]
        if profile in state["profiles"]:
            state["profiles"].remove(profile)
        finish()
    if args[0] == "allow":
        profile = args[1]
        assert profile.startswith("DevBox-KVM-"), args
        text = (Path(os.environ["FIREWALL_TEST_PROFILES"]) /
                ("devbox-kvm-" + profile.removeprefix("DevBox-KVM-"))).read_text()
        assert "ports=" + profile.removeprefix("DevBox-KVM-") + "/tcp" in text
        if profile not in state["profiles"]:
            state["profiles"].append(profile)
        finish()
    raise AssertionError(args)
raise AssertionError((name, args))
'''
    for name in ("firewall-cmd", "firewall-offline-cmd", "ufw", "id", "install"):
        path = commands / name
        path.write_text(dispatcher)
        path.chmod(0o755)
    helper = root / "configure-firewall"
    helper.write_text(source.replace(
        "PATH=/usr/sbin:/usr/bin:/sbin:/bin", "PATH=" + str(commands) + ":/usr/bin:/bin"
    ).replace("state_dir=/var/lib/devbox-kvm-firewall", "state_dir=" + str(state_dir)
    ).replace("ufw_app_dir=/etc/ufw/applications.d", "ufw_app_dir=" + str(profiles)))
    environment = dict(os.environ, FIREWALL_TEST_STATE=str(state_file),
                       FIREWALL_TEST_CALLS=str(calls_file), FIREWALL_TEST_PROFILES=str(profiles),
                       FIREWALL_MANAGE="true", FIREWALL_ZONE="")

    def set_state(**overrides):
        state = dict(firewalld=False, ufw=False, default_zone="public", zones=["external"],
                     ports=[], profiles=[], manual_rules=["22/tcp", "443/tcp"], fail="")
        state.update(overrides)
        state_file.write_text(json.dumps(state))

    def state():
        return json.loads(state_file.read_text())

    def run(listen="0.0.0.0:443", remove=False, expected=0, **extra):
        calls_file.write_text("")
        result = subprocess.run(["sh", str(helper), *(["--remove"] if remove else [])],
                                env=dict(environment, LISTEN_ADDR=listen, **extra),
                                capture_output=True, text=True)
        assert result.returncode == expected, (result.returncode, result.stderr)
        assert "Traceback" not in result.stderr, result.stderr
        calls = [json.loads(line) for line in calls_file.read_text().splitlines()]
        assert not any(arg in ("--reload", "--complete-reload", "--runtime-to-permanent",
                              "enable", "disable", "reset", "flush")
                       for call in calls for arg in call[1:]), calls
        return result, calls

    # Both firewall planes and all selected zones, including default.
    set_state(firewalld=True)
    run()
    expected_ports = {f"{plane}/{zone}/443" for plane in ("runtime", "permanent")
                      for zone in ("public", "external")}
    assert set(state()["ports"]) == expected_ports, state()
    _, calls = run()
    assert not any("--add-port=" in arg or "--remove-port=" in arg for call in calls for arg in call)
    run("0.0.0.0:8443", FIREWALL_ZONE="internal")
    assert set(state()["ports"]) == {"runtime/internal/8443", "permanent/internal/8443"}
    run(remove=True)
    assert state()["ports"] == []

    # Pre-existing administrator rules must survive configuration changes/removal.
    set_state(firewalld=True, ports=["runtime/public/443", "permanent/public/443"])
    run(FIREWALL_ZONE="public")
    run("0.0.0.0:9443", FIREWALL_ZONE="public")
    run(remove=True)
    assert set(state()["ports"]) == {"runtime/public/443", "permanent/public/443"}

    # UFW application rules remain distinct from manual numeric-port rules.
    set_state(ufw=True)
    run()
    assert state()["profiles"] == ["DevBox-KVM-443"]
    assert state()["manual_rules"] == ["22/tcp", "443/tcp"]
    run()
    assert state()["profiles"] == ["DevBox-KVM-443"]
    run("[::]:8443")
    assert state()["profiles"] == ["DevBox-KVM-8443"]
    assert not (profiles / "devbox-kvm-443").exists()
    run(remove=True)
    assert state()["profiles"] == []
    assert state()["manual_rules"] == ["22/tcp", "443/tcp"]

    # An existing unowned profile is not overwritten.
    foreign_profile = profiles / "devbox-kvm-443"
    foreign_profile.write_text("[Existing]\nports=1234/tcp\n")
    run(expected=1)
    assert foreign_profile.read_text() == "[Existing]\nports=1234/tcp\n"
    foreign_profile.unlink()
    set_state(ufw=True, foreign_profiles=["DevBox-KVM-443"])
    run(expected=1)
    assert not foreign_profile.exists() and not state()["profiles"]

    # Both managers can enforce input filtering concurrently.
    set_state(firewalld=True, ufw=True)
    run()
    assert set(state()["ports"]) == expected_ports
    assert state()["profiles"] == ["DevBox-KVM-443"]
    before = state()
    result, calls = run("0.0.0.0:8443", FIREWALL_MANAGE="false")
    assert state() == before and not calls
    run("127.0.0.1:8443")
    assert not state()["ports"] and not state()["profiles"]
    for listen in ("[::1]:443", "localhost:443", "[::ffff:127.0.0.1]:443"):
        run(listen)
        assert not state()["ports"] and not state()["profiles"]

    for listen in ("0.0.0.0", "0.0.0.0:https", "0.0.0.0:0", "0.0.0.0:65536",
                   "0.0.0.0:443;touch /tmp/unwanted"):
        run(listen, expected=1)
        assert not state()["ports"] and not state()["profiles"]
    run("0.0.0.0:00443")
    assert set(state()["ports"]) == expected_ports
    run(remove=True)

    # Inactive managers are not enabled; offline cleanup keeps ownership narrow.
    set_state()
    result, _ = run()
    assert "no active firewalld or UFW" in result.stderr
    assert not state()["ports"] and not state()["profiles"]
    set_state(firewalld=True, ufw=True)
    run()
    saved = state()
    saved.update(firewalld=False, ufw=False)
    state_file.write_text(json.dumps(saved))
    # A temporarily stopped manager must retain this listener's saved rules.
    _, calls = run()
    assert state() == saved
    assert not any(call[0] == "firewall-offline-cmd" for call in calls)
    result, _ = run(remove=True)
    assert set(state()["ports"]) == {"runtime/public/443", "runtime/external/443"}
    assert not state()["profiles"]
    assert "retained runtime ownership" in result.stderr
    assert len(list((state_dir / "firewalld/runtime").glob("*/*"))) == 2
    saved = state()
    saved["firewalld"] = True
    state_file.write_text(json.dumps(saved))
    run(remove=True)
    assert not state()["ports"]
    assert not list((state_dir / "firewalld/runtime").glob("*/*"))

    # Administrator-deleted zones are absent during cleanup, but invalid for new rules.
    set_state(firewalld=True)
    run(FIREWALL_ZONE="custom")
    saved = state()
    saved.update(ports=[], deleted_zones=["custom"])
    state_file.write_text(json.dumps(saved))
    run(FIREWALL_ZONE="public")
    assert set(state()["ports"]) == {"runtime/public/443", "permanent/public/443"}
    assert not list((state_dir / "firewalld").glob("*/custom/*"))
    run(remove=True)
    run(FIREWALL_ZONE="custom", expected=1)
    assert not state()["ports"]

    # Offline cleanup also accepts a zone deleted from saved configuration.
    set_state(firewalld=True)
    run(FIREWALL_ZONE="custom")
    saved = state()
    saved.update(firewalld=False, ports=[], deleted_zones=["custom"])
    state_file.write_text(json.dumps(saved))
    run(remove=True)
    assert not list((state_dir / "firewalld/permanent").glob("custom/*"))
    assert list((state_dir / "firewalld/runtime").glob("custom/*"))
    saved["firewalld"] = True
    state_file.write_text(json.dumps(saved))
    run(remove=True)
    assert not list((state_dir / "firewalld").glob("*/custom/*"))

    # Fail visibly and retain cleanup information after a partially applied rule.
    set_state(firewalld=True, fail="--add-port=443/tcp")
    result, _ = run(expected=1)
    assert "cannot open configured port" in result.stderr
    saved = state()
    saved["fail"] = ""
    state_file.write_text(json.dumps(saved))
    run()
    assert set(state()["ports"]) == expected_ports
    run(remove=True)

print("Firewall tests passed: scoped ports, both managers, ownership, persistence, cleanup, and errors.")
PY
