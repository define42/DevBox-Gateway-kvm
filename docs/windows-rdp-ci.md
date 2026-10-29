# Windows-only RDP compatibility CI

**Live Windows validation is still pending.**

The [Windows-only RDP workflow](../.github/workflows/rdp-windows.yml)
runs automatically as part of [CI](../.github/workflows/ci.yml), and can also be
started manually. It runs both our Go RDP test server and Microsoft's RDP ActiveX
client on **one Windows runner**, over loopback. No Linux machine, KVM, external gateway, LDAP
account, GitHub environment, or repository secrets are needed.

The workflow builds the server from the checked-out commit. It uses the
production `internal/rdp` implementation and software H.264 encoder, with a
generated desktop replacing the VM framebuffer. Unlike a test against a
predeployed gateway, it exercises the RDP code in the selected commit directly.
The fixture server lives in [scripts/rdp-windows/server](../scripts/rdp-windows/server/)
and [RunLocal.ps1](../scripts/rdp-windows/RunLocal.ps1) manages the local session.

## Run the test

CI runs the live test on pushes to `main`, pull requests targeting `main`, and
the nightly schedule. Look for **Microsoft RDP live picture and input**, with
separate **Live Microsoft RDP (h264)** and **Live Microsoft RDP (bitmap)** jobs
on `windows-2022`. Both must pass for the CI workflow to succeed. The separately
named **Check Windows RDP test harness (offline)** job only checks the tools;
its green result alone is not a live RDP compatibility result.

To rerun the same live test manually, in **Actions**, select
**Windows-only RDP compatibility** and choose **Run workflow**:

1. Select the branch containing the code to test.
2. Select **windows-2022** (the default) or **windows-2025**.
3. Leave **mode** set to **both** to test H.264 and bitmap, or select one mode.

No test target needs to be provisioned or prepared. The workflow starts a fresh
local test server for each mode and cleans up its process and temporary
certificate trust afterward. Automatic and manual runs share the same workflow
implementation at the tested commit; CI does not substitute self-tests for the
live connection. Release-job dependencies are unchanged: a live-test failure
fails CI, but does not currently block the separate release job from publishing.

These runner labels select Windows Server images, not an exact reproduction of
a Windows 11 workstation. See GitHub's
[Windows runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).

## What must pass

The server generates a 1024 × 768 image with four colored quadrants. The test
connects using the
[Microsoft RDP ActiveX client component](https://learn.microsoft.com/en-us/windows/win32/termserv/remote-desktop-activex-control)
and requires all of the following:

- The expected pattern appears in the rendered client window.
- Sending F8 through the client changes the pattern on the server and the new
  pattern appears in the client, demonstrating a keyboard round trip.
- The session stays connected for at least 30 seconds, with a changing corner
  heartbeat proving that rendering continues rather than freezing after F8.
- Server evidence confirms the intended active encoder. An H.264 connection
  that falls back to bitmap does not count as H.264 coverage.

An authenticated/connected event alone cannot pass the test. Neither can an
empty or black window. The fixture needs no browser, guest operating system, or
manually focused desktop page.

The live harness requires an interactive graphical desktop and fails if one is
unavailable; it does not skip rendering checks or substitute an offline test.
The [October 29 run](https://github.com/define42/DevBox-Gateway-kvm/actions/runs/40928606998)
passed temporary certificate installation/removal, the interactive desktop
capture probe, and actual Microsoft ActiveX control activation in both modes.
Both clients reported connection and logon-complete events, confirming receipt
of the new logon notification. They still disconnected before a verified picture:
bitmap reported `4868` (`0x1304`) after about 505 ms, and H.264 reported `3334`
(`0x0D06`) after about 46 ms, each with extended reason `0`. Microsoft's error
descriptions identify protocol errors but do not identify the offending packet.
**Live rendering, input, and stability validation remain pending.** Local script
tests and cross-compilation do not prove that these Windows failures are resolved.

`Test.ps1` runs two client checks before the live test. `--self-test` checks the
profile parser and fixture pixel logic. On Windows, `--check-control` initializes
and disposes the real Microsoft ActiveX control without calling `Connect` or
using credentials. This component check catches activation failures earlier;
both it and the full live connection test must pass.

## Scope and security

The test server listens only on `127.0.0.1`, using a dynamically allocated port.
Each run generates a temporary self-signed localhost TLS certificate. The
orchestrator imports only its public certificate into `LocalMachine\Root` for
the duration of the test, then removes it during cleanup. This operation requires
Windows, `GITHUB_ACTIONS=true`, `RUNNER_ENVIRONMENT=github-hosted`, an elevated
Administrator token, and the workflow's explicit
`RDP_TEST_ALLOW_MACHINE_TRUST=1` opt-in. The helper refuses other environments;
it does not request elevation or change certificate policy. GitHub documents
the hosted-runner marker in its
[variables reference](https://docs.github.com/en/actions/reference/workflows-and-actions/variables).

Import and removal each run in a child PowerShell process with a 20-second
deadline and up to 5 seconds to wait for a terminated child. An ownership journal
is written after verifying that the certificate is absent and before import,
so cleanup can remove it even if import times out after adding it. Cleanup
requires the matching journal and exact public certificate; it never removes a
preexisting certificate. Certificate verification remains enabled throughout.

The fixture uses dummy test credentials, not LDAP credentials. Its purpose is to
exercise the RDP protocol and display/input paths; it is **not an authentication
server** and must not be exposed outside loopback or reused as a production
endpoint.

This tests the Microsoft ActiveX component, not the `mstsc.exe` or Windows App
user interface, saved credential behavior, or password prompts. It also does
**not** test Linux KVM, VM boot, virtio framebuffer capture, the gateway's HTTPS
login/routing grants/TLS proxy, LDAP validation, or a real guest desktop.

For that additional full-path coverage, the separate
[Windows RDP gateway compatibility workflow](windows-rdp-gateway-ci.md) connects
Windows to isolated Linux/KVM test gateways. It still requires external targets
and protected credentials; those are not prerequisites for this Windows-only
test.

## Evidence and troubleshooting

Each mode uploads test evidence as an Actions artifact retained for **7 days**:

- `result.json`: overall stage and outcome, plus `cleanupErrors` if cleanup fails.
- `progress.json`: the latest stage, written atomically before each stage starts,
  so evidence is available even if the job is cancelled before producing a result.
- `server.json`: local server diagnostics and graphics-mode evidence.
- `client-events.json`: Microsoft client events, version metadata, attempted
  control class IDs, numeric activation HRESULTs, and disconnect reasons. For
  the generated loopback fixture only, a disconnect also includes Microsoft's
  `GetErrorDescription` result when available, limited to 1024 characters.
- `windows-events.json`: selected Windows RDP event identifiers, timestamps, and
  numeric error/reason fields from either `EventData` or `UserData`. A malformed
  record is marked individually so later records can still be exported.
- PNG screenshots of the rendered client content and pixel-check evidence.

`server.json` records build metadata, `actualEncoder` (`software` for H.264 or
`bitmap`), `sessionBitsPerPixel`, frames sent, input transitions, and session
duration. Its `completed`
outcome represents only the server's checks. Overall success additionally
requires the Microsoft client to pass rendering, input, and stability checks;
the server sending frames is not proof that Windows displayed them.
`framesSent` counts complete successful frame writes. A zero value does not
exclude a disconnect while the first frame is being written.

The production server sends a Save Session Info plain
logon notification after the initial Font Map, before graphics can be written.
Display reactivation does not repeat the logon notification. Its uncompressed
Share Data length field also follows Microsoft's server wire examples for
[Granted Control](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpbcgr/d5624241-aa4d-465a-b66d-698b19d0d826)
and [Plain Notify](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpbcgr/daf019f8-13c6-4781-9fd9-5395509b2efb).
Regression tests cover the notification ordering and packet lengths. These
changes did not resolve the disconnects in the run above.

The next correction selects the session color depth from Client Core Data
before Demand Active and keeps the advertised depth throughout the session.
Previously the server advertised 24 bpp, then replaced its output depth with the
client's Confirm Active preference, which could be 32 bpp. That client field is
a preference, not confirmation that the server's selected depth has changed;
see Microsoft's [Bitmap Capability Set](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpbcgr/76670547-e35c-4b95-a242-5729a21b83f6).
GFX is enabled only for 32-bpp sessions, consistent with
[xrdp's fix for Microsoft client failures at lower depths](https://github.com/neutrinolabs/xrdp/commit/d23d147dc775dbee9c8fe678d4549e9937cbce06).
Tests cover differing client/server preferences and display reactivation. A new
Windows run must still establish whether this mismatch caused either failure.
The report snapshots graphics readiness again at session exit so a disconnect
before the first frame tick does not leave that field stale.

If the server reports `client_not_connected` and there are no client events,
check `progress.json` before investigating protocol negotiation. The earlier
`CurrentUser\Root` import could wait for a Windows certificate consent dialog,
preventing the Microsoft client from starting. That is the likely cause of the
initial hosted-runner timeout; it is consistent with the
[documented native Windows behavior reported to .NET](https://github.com/dotnet/runtime/issues/24160).
The guarded machine-store import avoids that user consent path, and the child
deadlines prevent trust setup from hanging the whole test. The October 29 run
linked above confirms that this setup and cleanup now complete on Windows.

If client events report `microsoft_rdp_control_unavailable`, the client has not
yet reached the RDP server. The earlier discovery code treated COM class names
as registered ProgIDs. The harness now attempts actual `AxHost` activation using
Microsoft's documented class IDs for
[MsRdpClient10NotSafeForScripting](https://learn.microsoft.com/en-us/windows/win32/termserv/imsrdpclient10),
then [versions 9 and 8](https://learn.microsoft.com/en-us/windows/win32/termserv/imsrdpclient8).
It falls back only for `REGDB_E_CLASSNOTREG`; other activation errors fail the
check. Class IDs and numeric HRESULTs distinguish missing registration from
other activation failures without exporting raw exception messages.

Temporary private keys, credentials, downloaded/generated profiles, arbitrary
Windows event messages, and memory dumps are not artifact inputs. The local
server displays only generated fixture colors, not a user's desktop. Review
artifacts before sharing and avoid adding raw packets or unrestricted logs to
the artifact directory while investigating a failure.

Regular CI also compiles, self-tests, and checks control activation in the
explicitly offline harness job. Those checks make no RDP connection and do not
establish Microsoft RDP compatibility. Only the separate live jobs' render,
input, heartbeat, and stability passes provide that evidence.
