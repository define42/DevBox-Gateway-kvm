# Windows RDP gateway compatibility CI

The manually triggered [Windows RDP gateway workflow](../.github/workflows/rdp-gateway-windows.yml)
uses Microsoft's RDP ActiveX client on a Windows runner to connect to two
preconfigured Linux devbox-kvm test gateways. It tests rendering, keyboard input,
and a sustained connection in both H.264 and bitmap-only configurations.

This is the optional end-to-end test. For a self-contained test of the checked-out
RDP implementation, with no Linux host, gateway, or credentials to configure, use
[Windows-only RDP compatibility CI](windows-rdp-ci.md).

This is a client compatibility test, not a deployment workflow. Checking out a
commit does **not** deploy that commit to either gateway. The gateway build
reported by `/perf` is the build under test; deploy your intended build to both
test targets before running the workflow. The workflow never provisions,
restarts, reconfigures, or deletes a gateway or VM.

The test covers the [Microsoft RDP ActiveX client component](https://learn.microsoft.com/en-us/windows/win32/termserv/remote-desktop-activex-control),
not the `mstsc.exe` or Windows App user interface, credential prompt behavior, or
every Windows client version. Authentication and an `OnConnected` event alone do
not establish that a desktop was displayed.

Regular CI also compiles and self-tests the harness on `windows-2022`, without
credentials, a gateway, or a graphical desktop. This offline job is separate from
the existing release dependencies; only the manual workflow contacts test targets.
The live harness explicitly uses a 1024 × 768 window and supplies credentials
programmatically instead of exercising the downloaded profile's password dialog.

## Prepare isolated targets

1. Provision two **test-only** Linux gateway instances with the normal
   [KVM and confinement requirements](../README.md#host-and-image-requirements).
   Use disposable desktops containing no personal or production data. Both
   instances must run the build you intend to test.
2. Leave H.264 enabled on the first gateway (`GATEWAY_RDP_BITMAP_ONLY=false`).
   Configure `GATEWAY_RDP_BITMAP_ONLY=true` on the second gateway before starting
   its VM worker. This option is gateway-wide, so two VMs on a single gateway do
   not provide the two configurations. See the
   [bitmap-only diagnostic instructions](runtime.md) for restart requirements.
3. Configure a dedicated, non-administrator LDAP test account with the same
   username and password on both targets. Create one desktop owned by that
   account on each gateway. Do not reuse the public development credentials or
   a production account.
4. Configure HTTPS URLs and the RDP public address with certificates trusted by
   Windows. Both hostname validation and the certificate chain must succeed;
   the test does not bypass certificate checks or accept certificate warnings.
   Use the gateway's root HTTPS URL, including its port if non-default. The
   downloaded RDP address must have the same hostname and port as that URL;
   configure the gateway's public address accordingly.
5. Ensure the Windows runner can reach the HTTPS and RDP endpoints. The gateway
   must see the **same client source IP** for web login/profile download and RDP
   connection. A separate HTTP proxy or TLS-terminating frontend can violate
   this condition. The workflow performs both operations on the Windows runner
   and does not use an HTTP proxy or follow redirects; do not download profiles
   elsewhere and pass them to CI.
6. Start both desktops manually and prepare the browser fixture below. Do not
   connect another viewer or use the test desktops while CI is running. The
   account should own only these isolated test desktops.

A private network or tunnel is preferable when available, but this workflow does
not provision one. Arrange its connectivity separately; do not expose production
RDP or broadly open a production firewall for this test. Standard hosted runner
egress addresses are not fixed. See GitHub's
[runner networking reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners#ip-addresses)
before choosing a network access policy.

## Prepare the known desktop image

Copy [desktop.html](../scripts/rdp-windows/desktop.html) to each test guest. Open
the local file in a browser in the logged-in graphical guest session, enter
fullscreen mode (usually **F11**), and click the page to give it keyboard focus.
There are no external assets or network dependencies. A kiosk browser session is
also suitable if it leaves F8 available to the page.

Use a 1024 × 768 guest display for the baseline test, with browser zoom at 100%.
The fixture fills any viewport, but a mismatch between the guest display and the
client's 1024 × 768 desktop can introduce letterboxing or clipping. Keep panels,
browser toolbars, dialogs, screen lockers, screensavers, and notifications out of
the image. Disable automatic locking and display blanking only in these
disposable test sessions.

The screen is four solid-color quadrants. The client checks points at 25% and
75% of the rendered width and height, with a tolerance for compression:

| Quadrant | Initial RGB | After F8 RGB |
| --- | --- | --- |
| Top left | `220, 40, 40` (red) | `40, 80, 220` (blue) |
| Top right | `40, 200, 60` (green) | `220, 190, 40` (yellow) |
| Bottom left | `40, 80, 220` (blue) | `220, 40, 40` (red) |
| Bottom right | `220, 190, 40` (yellow) | `40, 200, 60` (green) |

Press F8 manually once to check that all four quadrants change; press it again to
restore them. Reloading resets the initial pattern. The automated test accepts
either pattern as its starting state and requires an observed change after
sending F8, so a prior run does not require a manual reset. A blank window or a
successful login with no graphics cannot pass this check.

## Configure GitHub

Create a GitHub Actions environment named **`rdp-compatibility`**. Restrict
deployment branches to `main` and enable required reviewers where your
GitHub plan supports them. Review the workflow and scripts before allowing a ref
to access this environment. This workflow is manual-only; do not add a
`pull_request_target` trigger or expose its credentials to untrusted pull
requests.

Set these repository variables (or equivalent variables in that environment):

| Variable | Value |
| --- | --- |
| `RDP_TEST_H264_URL` | Root HTTPS URL of the H.264 test gateway |
| `RDP_TEST_H264_VM_ID` | Running test desktop ID owned by the test account on that gateway |
| `RDP_TEST_BITMAP_URL` | Root HTTPS URL of the bitmap-only test gateway |
| `RDP_TEST_BITMAP_VM_ID` | Running test desktop ID owned by the test account on that gateway |

Set these **environment secrets**:

| Secret | Value |
| --- | --- |
| `RDP_TEST_USERNAME` | Dedicated non-administrator test account username |
| `RDP_TEST_PASSWORD` | That account's password, valid on both targets |

The test logs in and obtains a fresh short-lived, single-use routing grant for
each attempt. Never store a downloaded `.rdp` file as an Actions secret or
artifact. The test account must have permission to access the specified desktop;
being an administrator is not a substitute for ownership.

## Run and interpret the result

In **Actions**, select **Windows RDP gateway compatibility** and choose **Run workflow**:

1. Select the **`main`** branch; the workflow rejects other refs.
2. Select `windows-2022` (the default) or `windows-2025` for the **runner** input.
3. Choose **both** modes (the default), or **h264** / **bitmap** to rerun only
   one target.
4. Check **confirm_isolated_target** only after confirming that the configured
   targets and account are dedicated test systems. Its default is false and an
   unconfirmed run fails before using credentials.
5. Approve the protected environment if your repository requires approval.

The two mode tests run sequentially. These runner images are Windows Server
client environments, not an exact reproduction of a Windows 11 workstation.
GitHub documents the available [Windows runner labels](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).

Each graphics-mode test must show the known desktop pattern, demonstrate the
F8-triggered change through the remote keyboard path, and remain connected for
at least 30 seconds. The workflow collects gateway performance/diagnostic
snapshots and records the Windows client version to help correlate a failure
with the tested server build. Both requested and active encoder telemetry must
match: `software` for the H.264 target and `bitmap` for the bitmap target. A
configuration-only match or an H.264 connection falling back to bitmap is not
accepted as H.264 coverage.

The harness checks that an interactive desktop is available and fails rather
than treating an unavailable display as a passing RDP test. Hosted Windows GUI
execution still requires an initial real workflow run to validate this setup;
local tests of the scripts are not proof that the Microsoft client connected.
If the hosted runner cannot provide the required graphical session, investigate
that runner limitation separately from an RDP failure. A dedicated self-hosted
Windows runner matching the affected user's Windows version is a possible
follow-up, but is not selected or configured by this workflow.

Failures before rendering can be target readiness, connectivity, certificate,
login, routing-grant, runner desktop, encoder negotiation, or RDP protocol
failures. A fixture mismatch can also mean the guest browser lost focus or is no
longer fullscreen. Inspect the recorded evidence before assigning a root cause.

## Artifacts and privacy

Artifacts are retained for **7 days** and are intended to contain only the test
result, client/version metadata, client screenshot evidence, selected Windows
RDP event data, and sanitized gateway diagnostics. Credentials, authentication
cookies, routing grants, and raw downloaded `.rdp` files must not be uploaded.

`result.json` records the overall stage/outcome and whether the active encoder
was observed. `client-events.json` records Microsoft client events and numeric
disconnect reasons. `gateway-before.json`, `gateway-active.json` (when observed),
and `gateway-after.json` contain owner-scoped `/perf` snapshots, including the
running gateway build. `windows-events.json` contains only event identifiers,
timestamps, and numeric error/reason fields—not arbitrary Windows messages.
Screenshots are cropped to the client content; an unrecognized terminal image is
replaced with averaged quadrant colors to avoid capturing login/certificate text.

**Screenshots can contain whatever is on the remote test desktop.** Redacted
gateway diagnostics still contain VM identifiers, versions, timings, and usage
data. Use only isolated fixture desktops, review artifacts before sharing, and
remember that repository readers may be able to download workflow artifacts.
Do not add raw HTTP traces, arbitrary event-message dumps, or memory dumps to
the artifact directory when troubleshooting authentication.

The fixture's local contract tests require only Node.js:

```sh
node --test scripts/rdp-windows/desktop.test.cjs
```

They verify the colors, keyboard state transitions, reset behavior, and
self-contained layout contract; they do not replace the Windows render test.
