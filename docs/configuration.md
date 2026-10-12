# Gateway configuration

`gokvm serve` reads all gateway settings from environment variables through
[`internal/config/config.go`](../internal/config/config.go), following the
[DevBox-Gateway configuration design](https://github.com/define42/DevBox-Gateway/blob/main/internal/config/config.go).
Set exported variables in your service environment or pass them before the command:

```bash
LDAP_URL=ldaps://directory.example.com \
LDAP_BASE_DN=dc=example,dc=com \
LDAP_USER_DOMAIN=example.com \
ADMIN_GROUP=devbox-admins \
./gokvm serve
```

Exactly one of `LDAP_URL` and `AUTH_FILE` must be set. Authentication, TLS,
paths, and supported value ranges are validated during startup. See the
[gateway guide](gateway.md) for LDAP, certificates, VM admission, and storage.

Precedence is **explicit command-line flag → environment variable → default**.
Existing flags remain supported, including explicit false and empty overrides.
Surrounding whitespace is trimmed from string environment values; an explicitly
empty value remains empty. Malformed integer, boolean, or duration environment
values fall back to their registered defaults. Parsed values still undergo the
gateway's normal startup validation and default handling. For example, zero
selects component defaults for CPU count, memory, quotas, session limits, and
several timeouts. Durations use Go syntax such as `30s`, `10m`, and `8h`.
Settings are read at startup; restart the gateway after changing them.

The native executable does **not** automatically load a `.env` file. Docker
Compose loads `.env` and passes the listed settings into the container. For
`docker run`, supply `--env NAME=value` or `--env-file /path/to/gateway.env`.
An env file for `docker run` should contain literal `NAME=value` assignments,
without shell expansion or quote characters around values.

## Storage and desktops

Defaults below are for the native executable. The Docker image changes
`DATA_ROOT_DIR` to `/var/lib/gokvm` and `LISTEN_ADDR` to `0.0.0.0:8443`.

| Environment variable | Default | Flag override | Purpose |
| --- | --- | --- | --- |
| `DATA_ROOT_DIR` | `.gokvm-gateway` | `-state` | Private persistent state directory. |
| `BASE_IMAGE` | Empty | `-image` | Optional immutable raw or supported uncompressed QCOW2 default template. Omit to use administrator uploads. |
| `VM_BINARY` | Empty | `-vm-binary` | Executable for managed VMs; empty uses the current executable. |
| `UEFI_CODE` | `/usr/share/OVMF/OVMF_CODE_4M.fd` | `-uefi` | OVMF CODE firmware. |
| `UEFI_VARS_TEMPLATE` | `/usr/share/OVMF/OVMF_VARS_4M.fd` | `-uefi-vars-template` | Matching pristine variables template, copied per VM. |
| `H264_ENCODER` | `auto` | `-encoder` | `auto`, `nvenc`, or `software`. |
| `RDP_PROFILE` | `office` | `-rdp-profile` | AVC420 cap: `office` (30 FPS) or `smooth` (60 FPS); bitmap fallback stays at 30 FPS. Applies when a managed VM starts. |
| `VM_KEYBOARD` | `us` | `-keyboard` | Keyboard layout provisioned in new guests. |
| `VM_VCPU_COUNT` | `2` | `-cpus` | vCPUs per desktop, from 1 to 64. |
| `VM_MEMORY_MIB` | `4096` | `-memory` | MiB per desktop, from 256 to 1048576. |
| `VM_MEMORY_BUDGET_MIB` | `0` | `-memory-budget` | Total desktop memory budget in MiB; 0 selects automatic capacity. An explicit value can lower that budget. |
| `MAX_VDI_PER_USER` | `3` | `-max-vms-per-user` | Desktop quota per account. |
| `MAX_CONCURRENT_CREATIONS` | `2` | `-max-creations` | Gateway-wide simultaneous desktop provisioning limit, from `1` to `64`. Further requests wait within `VM_CREATE_TIMEOUT`. |
| `MAX_RUNNING_VMS` | `4` | `-max-running` | Global running desktop limit. |
| `IMAGE_UPLOAD_MAX_BYTES` | `21474836480` | `-image-upload-limit` | Maximum bytes per uploaded or normalized catalog file (20 GiB), up to 1 TiB. |
| `IMAGE_UPLOAD_TIMEOUT` | `30m` | `-image-upload-timeout` | Overall upload/import timeout, from `1m` to `24h`. |
| `VM_CREATE_TIMEOUT` | `30m` | `-vm-create-timeout` | Overall desktop provisioning timeout including queue wait, from `1m` to `24h`. |
| `VM_START_TIMEOUT` | `30s` | `-vm-start-timeout` | Time allowed for a managed VM to start its RDP console. |
| `VM_STOP_TIMEOUT` | `30s` | `-vm-stop-timeout` | Time allowed for managed VM shutdown. |

## Listener and TLS

| Environment variable | Default | Flag override | Purpose |
| --- | --- | --- | --- |
| `LISTEN_ADDR` | `127.0.0.1:8443` | `-listen` | Shared HTTPS/RDP TCP listener. |
| `PUBLIC_ADDRESS` | `localhost:8443` | `-public-address` | Hostname and port used by the gateway, its certificate, and downloaded RDP files. |
| `CERT_FILE` | Empty | `-tls-cert` | Optional PEM certificate; supply together with `KEY_FILE`. |
| `KEY_FILE` | Empty | `-tls-key` | Matching private PEM key. |
| `ACME_ENABLE` | `false` | `-acme` | Obtain and renew a public certificate with TLS-ALPN-01 on public TCP 443. Excludes supplied PEM files. |
| `ACME_EMAIL` | Empty | `-acme-email` | ACME account contact email. |
| `ACME_CA` | `production` | `-acme-ca` | `production`/`prod`, `staging`, or custom ACME directory URL. |

Without supplied PEM files or ACME, the gateway generates a persistent
self-signed certificate. Enabling ACME accepts the selected CA's subscriber
agreement. See [certificates and ACME](gateway.md#gateway-certificates-and-acme).

## Authentication and sessions

| Environment variable | Default | Flag override | Purpose |
| --- | --- | --- | --- |
| `AUTH_FILE` | Empty | `-auth-file` | Private bcrypt JSON file for loopback development. Excludes LDAP and grants no admin role. |
| `LDAP_URL` | Empty | `-ldap-url` | Directory URL, `ldaps://` or `ldap://` (StartTLS). |
| `LDAP_BASE_DN` | Empty | `-ldap-base-dn` | Required LDAP search base DN. |
| `LDAP_USER_DOMAIN` | Empty | `-ldap-domain` | Domain appended to the submitted username when binding, without a leading `@`. |
| `LDAP_USER_FILTER` | Empty | `-ldap-filter` | Search filter. Empty selects `(uid=%s)`, or `(mail=%s)` when a domain is configured. |
| `LDAP_USERNAME_ATTRIBUTE` | `uid` | `-ldap-username-attribute` | Canonical VM-owner account attribute. |
| `LDAP_REQUIRED_GROUP` | Empty | `-ldap-required-group` | Optional direct group membership required for login. |
| `ADMIN_GROUP` | Empty | `-ldap-admin-group` | Optional direct group membership granting the administrator role. |
| `LDAP_CA_FILE` | Empty | `-ldap-ca` | Private CA PEM file; empty uses system trust. |
| `LDAP_INSECURE_LOOPBACK` | `false` | `-ldap-insecure-loopback` | Permit plaintext LDAP only with a loopback IP, for testing. |
| `LDAP_AUTH_TIMEOUT` | `10s` | `-ldap-timeout` | LDAP authentication timeout. |
| `SESSION_TTL` | `8h` | `-session-ttl` | Browser session lifetime, from `1m` to `24h`. |
| `MAX_SESSIONS` | `1024` | `-max-sessions` | Maximum browser sessions, from 1 to 10000. |

## Connection admission

| Environment variable | Default | Flag override | Purpose |
| --- | --- | --- | --- |
| `MAX_CONCURRENT_CONNECTIONS` | `128` | `-max-connections` | Maximum authenticated HTTPS/RDP connections, from 1 to 4096; excludes the pending pool. |
| `MAX_HANDSHAKES` | `64` | `-max-handshakes` | Maximum pending, unauthenticated connections, from 1 to 4096. |
| `MAX_CONNECTIONS_PER_IP` | `32` | `-max-connections-per-ip` | Maximum pending and authenticated connections combined per source, from 1 to 4096. |
| `MAX_HANDSHAKES_PER_IP` | `8` | `-max-handshakes-per-ip` | Maximum pending connections per source, from 1 to 4096. |
| `CONNECTION_RATE_LIMIT` | `128` | `-connection-rate-limit` | Global TCP admission rate per second, from 1 to 100000, with an equal-size burst. |
| `CONNECTION_RATE_LIMIT_PER_IP` | `8` | `-connection-rate-limit-per-ip` | TCP admission rate per source per second, from 1 to 100000, with an equal-size burst. |
| `HANDSHAKE_TIMEOUT` | `10s` | `-handshake-timeout` | Total time from TCP acceptance to application authentication, from `1s` to `1m`. |

Pending connections have separate capacity from authenticated connections. With
the defaults, the global ceiling is 64 pending plus 128 authenticated connections.
Completing TLS alone does not authenticate a connection: HTTPS must present a
valid browser session or complete login, and RDP must validate its credentials.
The pending timeout covers protocol negotiation, TLS, and application
authentication together; sending more bytes does not extend it. Zero selects the
default for these settings; it does not disable a limit.

Source limits use the actual TCP peer, not forwarded HTTP headers. IPv4-mapped
IPv6 addresses share their IPv4 bucket; IPv6 addresses share a bucket per /64.
Tune per-source limits for offices behind one NAT or IPv6 prefix, allowing for
browser parallel connections as well as RDP. The separate password-attempt
limits still apply. See [connection admission and edge protection](gateway.md#connection-admission-and-edge-protection)
for deployment requirements.

## Compose and existing deployments

The root [`compose.yaml`](../compose.yaml) passes every setting above to the
gateway and starts it with `serve`. [`.env.example`](../.env.example) includes
all settings and configures the bundled development LDAP directory. Its LDAP
address, CA path, account attributes, and group settings differ from the native
executable's empty-directory defaults. File paths in Compose are container paths;
add corresponding mounts when supplying certificates or an external base image.

`GATEWAY_BIND`, `GATEWAY_HOST`, `GATEWAY_PORT`, and `KVM_GID` remain Compose
host-deployment controls. `PUBLIC_ADDRESS` defaults to
`GATEWAY_HOST:GATEWAY_PORT` when it is unset. Keep the internal `LISTEN_ADDR`
port aligned with the Compose port mapping and the state directory aligned with
its persistent volume mount.

Existing Compose `.env` files continue to work. These legacy Compose variables
are used only when the corresponding canonical variable is unset:

| Legacy Compose variable | Canonical environment variable |
| --- | --- |
| `GATEWAY_ENCODER` | `H264_ENCODER` |
| `GATEWAY_CPUS` | `VM_VCPU_COUNT` |
| `GATEWAY_MEMORY` | `VM_MEMORY_MIB` |
| `GATEWAY_MAX_RUNNING` | `MAX_RUNNING_VMS` |
| `GATEWAY_ACME` | `ACME_ENABLE` |
| `GATEWAY_ACME_EMAIL` | `ACME_EMAIL` |
| `GATEWAY_ACME_CA` | `ACME_CA` |

Canonical variables take precedence even when explicitly empty. Rename legacy
entries when migrating; avoid retaining conflicting values. The executable
itself reads the canonical names only. The native `make gateway` workflow also
retains its existing Make helpers and aliases; see the
[development setup](gateway.md#local-development-setup).
