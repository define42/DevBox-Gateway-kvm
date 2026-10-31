# Windows-only RDP compatibility CI

The [October 29 compatibility run](https://github.com/define42/DevBox-Gateway-kvm/actions/runs/40938912570)
passed with Microsoft's ActiveX client. The separate
[performance benchmark](windows-rdp-performance.md) measures desktop throughput.
The [mstsc.exe workflow](../.github/workflows/rdp-mstsc-windows.yml) tests the actual
Windows Remote Desktop application. The
[October 31 validation run](https://github.com/define42/DevBox-Gateway-kvm/actions/runs/41228309144)
passed both bitmap and H.264 on Windows Server 2022 at commit `68ae3fc`, including
rendering, F8 input, a changing heartbeat for at least 30 seconds, and cleanup.

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
The mstsc.exe workflow uses the same fixture with `RDP_TEST_CLIENT=mstsc` and
launches the system `mstsc.exe` through a separate process harness. Both live
workflows are integration tests. The offline harness self-tests are unit tests
and cannot establish that the application connects or renders correctly.

## Run the test

CI runs the live test on pushes to `main`, pull requests targeting `main`, and
the nightly schedule. Look for **Microsoft RDP live picture and input**, with
separate **Live Microsoft RDP (h264)** and **Live Microsoft RDP (bitmap)** jobs
on `windows-2022`. **mstsc.exe live picture and input** independently runs
**Live mstsc.exe (h264)** and **Live mstsc.exe (bitmap)** against the same fixture.
All live jobs must pass for the CI workflow to succeed. The separately
named **Check Windows RDP test harness (offline)** job only checks the tools;
its green result alone is not a live RDP compatibility result.

To rerun the same live test manually, in **Actions**, select
**Windows-only RDP compatibility** for the ActiveX client or
**Windows mstsc.exe compatibility** for the executable, then choose
**Run workflow**:

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
in the original workflow, or the real `mstsc.exe` window in the executable
workflow. Both require all of the following:

- The expected pattern appears in the rendered client window.
- Sending F8 through the client changes the pattern on the server and the new
  pattern appears in the client, demonstrating a keyboard round trip.
- The session stays connected for at least 30 seconds, with a changing corner
  heartbeat proving that rendering continues rather than freezing after F8.
- Server evidence confirms the intended active encoder. An H.264 connection
  that falls back to bitmap does not count as H.264 coverage.

The executable test also shrinks the connected window and restores its original
size. On a 1024 × 768 runner desktop, this changes the outer window from
1024 × 728 to 800 × 600 and back; smaller desktops use a smaller target that
still leaves the fixture visible. At each size, the harness checks the measured
window bounds, waits for three stable fixture frames, sends a new F8, and
requires the resulting layout change plus at least four fresh heartbeat changes.
The server must record at least three input transitions in the same session.
The remote desktop stays at 1024 × 768 with smart sizing enabled and dynamic
resolution disabled; this checks client window resizing and scaled rendering.

An authenticated/connected event alone cannot pass the test. Neither can an
empty or black window. The fixture needs no browser, guest operating system, or
manually focused desktop page.

Both live harnesses require an interactive graphical desktop and fail if one is
unavailable; neither skips rendering checks or substitutes an offline test.
The [October 29 run](https://github.com/define42/DevBox-Gateway-kvm/actions/runs/40938912570)
passed the ActiveX compatibility test in both modes, including rendering,
F8 input, the changing heartbeat, session duration, and cleanup. This fixture
deliberately sends only two frames per second at 1024 × 768. Use the separate
[1080p performance baseline](windows-rdp-performance.md) to measure scrolling,
visible progress, input response times, and resource use under continuous load.

`Test.ps1` runs offline checks before the live tests. The client harnesses'
`--self-test` checks cover profile validation, fixture pixel logic, keyboard
submission helpers, and mstsc file-consent decisions. Consent cases include
static labels and read-only address fields, mismatched destinations, selected
redirections, ambiguous buttons, and credential/certificate prompts. PowerShell
tests cover client selection, orchestration, and rejection of missing or invalid
resize evidence. State tests require both resizes with fresh input and heartbeat
confirmation before a session can pass.
These checks do not start a live `mstsc.exe` session. On Windows, the ActiveX
harness's `--check-control` initializes and disposes the real Microsoft ActiveX control without calling `Connect` or
using credentials. This component check catches activation failures earlier;
both it and the live connection checks must pass.

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

The original workflow tests the Microsoft ActiveX component. The separate
mstsc.exe workflow launches the Windows application, captures its rendered
fixture, shrinks and restores the window, sends F8 at each size, and checks the
changing heartbeat during a 30-second session. It uses a generated loopback profile with certificate
verification enabled. It does not test Windows App, saved credential behavior,
or interactive password prompts. The fixture profile disables CredSSP because
the local server has no credential authentication; TLS server authentication
remains required. It never stores a password or connects to an external host.

The executable harness handles only the known English first-launch and local
file-consent dialogs described in [Microsoft's RDP security warning guide](https://learn.microsoft.com/en-us/windows-server/remote/remote-desktop-services/remotepc/understanding-security-warnings).
It checks the owning process and exact loopback destination, leaves resource
redirection disabled, and fails on other dialogs, including certificate or
credential prompts. The destination may be a static label or a labeled,
read-only UI Automation field; writable and password fields cannot authorize
consent. Server 2022's file warning exposes publisher, connection type, and
remote-computer fields but omits the port from its displayed address. For that
observed structure, the harness accepts the labeled `127.0.0.1` host and uses the
validated launch profile for the exact port. A displayed host and port must
still match exactly, and overall success requires evidence from the launched
fixture server. Each recognized dialog is accepted at most once per session. A Windows
job object terminates the launched client even if its observer times out.
Offline checks do not confirm live dialog behavior or window geometry; those
require successful Windows runs.

Neither workflow validates audio playback: the fixture disables audio. They also
do not test Linux KVM, VM boot, virtio framebuffer capture, the gateway's HTTPS
login/routing grants/TLS proxy, LDAP validation, or a real guest desktop.

For that additional full-path coverage, the separate
[Windows RDP gateway compatibility workflow](windows-rdp-gateway-ci.md) connects
Windows to isolated Linux/KVM test gateways. It still requires external targets
and protected credentials; those are not prerequisites for this Windows-only
test.

## Evidence and troubleshooting

Each mode uploads test evidence as an Actions artifact retained for **7 days**.
The executable workflow uses the `windows-only-rdp-mstsc-exe-` artifact prefix
and a separate `rdp-mstsc-exe-results` directory, so its results cannot be
mistaken for ActiveX results:

- `result.json`: overall stage and outcome, plus `cleanupErrors` if cleanup fails.
- `progress.json`: the latest stage, written atomically before each stage starts,
  so evidence is available even if the job is cancelled before producing a result.
- `server.json`: local server diagnostics and graphics-mode evidence.
- `client-events.json`: harness events and client evidence. The ActiveX harness
  includes version metadata, attempted control class IDs, numeric activation
  HRESULTs, and disconnect reasons. For the generated loopback fixture only,
  an ActiveX disconnect also includes Microsoft's `GetErrorDescription` result
  when available, limited to 1024 characters. The executable harness records
  process/window and rendering/input checks instead of ActiveX control events.
  Its `mstsc_dialog_rejected` entries identify the rejection reason and bounded
  control metadata, including ownership, visibility, toggle state, and read-only
  status. Only recognized labels are retained; unknown text and field values
  are omitted or redacted. If the fixture is not recognized after ten seconds,
  it also records native window classes and bounds and captures `mstsc-window.png`.
  Input diagnostics record the focused control class and verify process ownership.
  Each `window_resize_verified` entry records the shrink or restore phase,
  measured outer window and rendered fixture dimensions, before/after layouts,
  and fresh heartbeat evidence. `resize-shrink.png` and `resize-restore.png`
  preserve the rendered viewport at its actual captured size.
- `windows-events.json`: selected Windows RDP event identifiers, timestamps, and
  numeric error/reason fields from either `EventData` or `UserData`. A malformed
  record is marked individually so later records can still be exported.
- PNG screenshots of the rendered client content and pixel-check evidence.

`server.json` records build metadata, `actualEncoder` (`software` for H.264 or
`bitmap`), `sessionBitsPerPixel`, frames sent, input transitions, and session
duration. `keyDownEvents` and `keyUpEvents` count decoded keyboard events,
including repeats and keys other than F8, without recording key values. They
help distinguish missing keyboard delivery from a missing fixture transition.
Its `completed` outcome represents only the server's checks. Overall success
additionally requires the Microsoft client to pass rendering, input, and stability checks;
the server sending frames is not proof that Windows displayed them.
`framesSent` counts complete successful frame writes. A zero value does not
exclude a disconnect while the first frame is being written.

The production server sends a Save Session Info plain
logon notification after the initial Font Map, before graphics can be written.
Display reactivation does not repeat the logon notification. Its uncompressed
Share Data length field also follows Microsoft's server wire examples for
[Granted Control](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpbcgr/d5624241-aa4d-465a-b66d-698b19d0d826)
and [Plain Notify](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpbcgr/daf019f8-13c6-4781-9fd9-5395509b2efb).
Regression tests cover the notification ordering and packet lengths.

The server selects the session color depth from Client Core Data
before Demand Active and keeps the advertised depth throughout the session.
Previously the server advertised 24 bpp, then replaced its output depth with the
client's Confirm Active preference, which could be 32 bpp. That client field is
a preference, not confirmation that the server's selected depth has changed;
see Microsoft's [Bitmap Capability Set](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpbcgr/76670547-e35c-4b95-a242-5729a21b83f6).
GFX is enabled only for 32-bpp sessions, consistent with
[xrdp's fix for Microsoft client failures at lower depths](https://github.com/neutrinolabs/xrdp/commit/d23d147dc775dbee9c8fe678d4549e9937cbce06).
Tests cover differing client/server preferences and display reactivation. The
earlier [color-depth correction run](https://github.com/define42/DevBox-Gateway-kvm/actions/runs/40934733654)
confirmed H.264 rendering, while bitmap still failed with `0x1304`.
The report snapshots graphics readiness again at session exit so a disconnect
before the first frame tick does not leave that field stale.

Bitmap rectangles are now split when needed to keep the complete MCS Send Data
structure within the [16,383-byte protocol limit](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpbcgr/b61e1c76-5d7c-4285-b61a-b69f85e4a0e2).
Previously a 64 × 64 rectangle at 32 bpp produced 16,432 MCS bytes including
headers. Dirty-tile tracking still uses 64 × 64 tiles; oversized tiles are sent
as smaller row strips. Tests check independent wire lengths, pixel coverage,
and retry after a partially sent tile. The latest Windows run above confirms
bitmap rendering with this correction and no longer reports `0x1304`.

Before submitting F8, the ActiveX harness explicitly focuses the hosted RDP
control and records the numeric `SendKeys` HRESULT in `f8_submitted`. This event records API
submission only; `fixture_changed` remains the evidence of remote delivery.
The [earlier run](https://github.com/define42/DevBox-Gateway-kvm/actions/runs/40937321486)
confirmed focus and successful API submission, but still timed out waiting for
the F8 response; those checks alone did not resolve the failure.
The harness now submits the raw F8 scan code `0x42` for both press and release,
with the release state in the separate key-up array. Previously it supplied
`WM_KEYDOWN`-style values `0x00420001` and `0xC0420001`. The correction follows
the inspected `SendKeys` caller in Microsoft's
[RDCMan 3.22](https://learn.microsoft.com/en-us/sysinternals/downloads/rdcman) binary
and the raw-code usage in
[1Remote's client](https://github.com/1Remote/1Remote/blob/f1cfdd1fc518c236e5bd339d3f0b74452f8f9303/Ui/View/Host/ProtocolHosts/AxMsRdpClient09Host.xaml.cs),
whose [wrapper](https://github.com/1Remote/1Remote/blob/f1cfdd1fc518c236e5bd339d3f0b74452f8f9303/Ui/Utils/RdpFile/MsRdpClientNonScriptableWrapper.cs)
passes those values unchanged. This differs from the `lParam` description in
the API reference. The successful compatibility run above verifies the resulting
F8 round trip with the rendering, input, and stability requirements intact.

If the server reports `client_not_connected` and there are no client events,
check `progress.json` before investigating protocol negotiation. The earlier
`CurrentUser\Root` import could wait for a Windows certificate consent dialog,
preventing the Microsoft client from starting. That is the likely cause of the
initial hosted-runner timeout; it is consistent with the
[documented native Windows behavior reported to .NET](https://github.com/dotnet/runtime/issues/24160).
The guarded machine-store import avoids that user consent path, and the child
deadlines prevent trust setup from hanging the whole test. The October 29 run
linked above confirms that this setup and cleanup now complete on Windows.

If ActiveX client events report `microsoft_rdp_control_unavailable`, the client
has not yet reached the RDP server. The earlier discovery code treated COM class names
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

Regular CI also compiles and self-tests both harnesses, and checks ActiveX
control activation in the explicitly offline harness job. Those checks make no
RDP connection and do not establish Microsoft RDP compatibility. Only the separate live jobs' render,
input, heartbeat, and stability passes provide that evidence.
