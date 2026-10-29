# Windows-only RDP compatibility CI

The manually triggered [Windows-only RDP workflow](../.github/workflows/rdp-windows.yml)
runs both our Go RDP test server and Microsoft's RDP ActiveX client on **one
Windows runner**, over loopback. No Linux machine, KVM, external gateway, LDAP
account, GitHub environment, or repository secrets are needed.

The workflow builds the server from the checked-out commit. It uses the
production `internal/rdp` implementation and software H.264 encoder, with a
generated desktop replacing the VM framebuffer. Unlike a test against a
predeployed gateway, it exercises the RDP code in the selected commit directly.
The fixture server lives in [scripts/rdp-windows/server](../scripts/rdp-windows/server/)
and [RunLocal.ps1](../scripts/rdp-windows/RunLocal.ps1) manages the local session.

## Run the test

In **Actions**, select **Windows-only RDP compatibility** and choose **Run workflow**:

1. Select the branch containing the code to test.
2. Select **windows-2022** (the default) or **windows-2025**.
3. Leave **mode** set to **both** to test H.264 and bitmap, or select one mode.

No test target needs to be provisioned or prepared. The workflow starts a fresh
local test server for each mode and cleans up its process and temporary
certificate trust afterward. It is manual-only and is not a release gate.

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
**Hosted Windows graphical execution has not yet been validated by a live run.**
Local script tests and cross-compilation do not prove that the Microsoft client
connected or rendered successfully. A first-run desktop/environment failure
must be distinguished from an RDP protocol failure. If the hosted runner cannot
provide the required session, a dedicated interactive Windows runner is a
possible follow-up, not something this workflow provisions automatically.

## Scope and security

The test server listens only on `127.0.0.1`, using a dynamically allocated port.
Each run generates a temporary self-signed localhost TLS certificate. The
orchestrator imports only its public certificate into the current user's trusted
root store for the duration of the test, then removes it during cleanup.
Certificate verification remains enabled; certificate warnings are not accepted
as a test workaround.

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

- `result.json`: overall stage and outcome.
- `server.json`: local server diagnostics and graphics-mode evidence.
- `client-events.json`: Microsoft client events, version metadata, and numeric
  disconnect reasons.
- `windows-events.json`: selected Windows RDP event identifiers, timestamps, and
  numeric error/reason fields.
- PNG screenshots of the rendered client content and pixel-check evidence.

`server.json` records build metadata, `actualEncoder` (`software` for H.264 or
`bitmap`), frames sent, input transitions, and session duration. Its `completed`
outcome represents only the server's checks. Overall success additionally
requires the Microsoft client to pass rendering, input, and stability checks;
the server sending frames is not proof that Windows displayed them.

Temporary private keys, credentials, downloaded/generated profiles, arbitrary
Windows event messages, and memory dumps are not artifact inputs. The local
server displays only generated fixture colors, not a user's desktop. Review
artifacts before sharing and avoid adding raw packets or unrestricted logs to
the artifact directory while investigating a failure.

Regular CI separately compiles and self-tests the client harness on Windows
without a graphical desktop or live connection. Those checks catch harness
regressions but do not establish Microsoft RDP compatibility. A live render,
input, and stability pass is the evidence supplied by this manual workflow.
