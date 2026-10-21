# Local LDAP directory

This directory brings the DevBox-Gateway demo LDAP users and groups into
DevBox-Gateway-kvm.
[`default-config.cfg`](default-config.cfg) is copied unchanged from
[DevBox-Gateway commit `e4fbf205`](https://github.com/define42/DevBox-Gateway/blob/e4fbf205adb7663167190f1082c41ff81681d3d8/testldap/default-config.cfg),
with its [MIT license](LICENSE). Both Compose setups use a pinned GLAuth image
as their binary source and require TLS.

## Gateway and LDAP in Docker

The repository's root [`compose.yaml`](../compose.yaml) starts the gateway and
LDAP together. Follow the [Docker quick start](../docs/docker.md#start-the-gateway-and-ldap-with-compose)
to configure the KVM group, then run `docker compose up --build -d`
from the repository root. The gateway is published on `127.0.0.1:8443` by
default. LDAP is reachable only inside the Compose network as `ldaps://ldap:389`;
it has no published host port.

The LDAP container generates its own TLS identity on first startup and reuses
it on later starts. It shares only its public certificate with the gateway,
which verifies both the certificate and the `ldap` hostname. The gateway manages
its own self-signed or ACME certificate independently. The accounts and group rules below are shared by both
setups. `docker compose down` stops the complete container stack while
preserving its named volumes.

## Native gateway with LDAP in Docker

The separate [`testldap/compose.yaml`](compose.yaml) serves the native gateway
and tests through `127.0.0.1:1636`. Install Docker with the Compose plugin and
OpenSSL, then run from the repository root:

```bash
make gateway              # Start LDAP, then the native gateway
make ldap                 # Start LDAP alone
make gateway-ldap-test    # Run the real LDAP authentication tests
make ldap-stop            # Stop this repository's LDAP service
```

Stopping the gateway leaves LDAP running. `make ldap-stop` does not stop other
LDAP services or remove gateway workspaces. Override the host port with
`make gateway LDAP_PORT=2636` if needed.

Sign in to the gateway with the bare username, such as `johndoe`. The gateway
binds as `johndoe@example.com`, searches the `mail` attribute, and requires direct
membership in `ou=team10_r,ou=groups,dc=glauth,dc=com`.

| Username | Password | Gateway access |
| --- | --- | --- |
| `johndoe` | `dogood` | Allowed |
| `testuser` | `dogood` | Allowed |
| `administrator` | `dogood` | Administrator: direct member of `devbox-admins` |
| `serviceuser` | `mysecret` | Denied: outside the required group |

These are public demo credentials, retained from DevBox-Gateway for local use.
The `administrator` account can open **Admin** to upload and remove shared QCOW2
base images and start, stop, or delete any owner’s VM. Every allowed user can
select an uploaded image when creating a desktop. RDP downloads remain limited
to each VM’s owner, even for administrators. A new gateway starts with an empty
image catalog; sign in as `administrator` to upload the first template. Existing
VMs retain independent disks if an uploaded image is removed. An optional default
image configured with `-image` is protected from removal.
New guests use the authenticated username and password at their graphical login.
Existing guests retain their provisioned passwords and owners.

For the native workflow, `generate-cert.sh` creates a local certificate and private key in the ignored
`.tls/` directory, with `localhost` and `127.0.0.1` as certificate names. Compose
mounts these files into GLAuth, and the gateway trusts `.tls/cert.pem` explicitly.
Certificate verification stays enabled. The private key is generated locally;
it is not copied from DevBox-Gateway or committed to this repository.

For an external directory, disable the bundled service and set its connection
options, for example:

```bash
make gateway GATEWAY_LDAP_LOCAL=0 \
  GATEWAY_LDAP_URL=ldaps://directory.example.com \
  GATEWAY_LDAP_BASE_DN=dc=example,dc=com \
  GATEWAY_LDAP_DOMAIN=example.com \
  GATEWAY_LDAP_FILTER='(userPrincipalName=%s)' \
  GATEWAY_LDAP_USERNAME_ATTRIBUTE=sAMAccountName \
  GATEWAY_LDAP_REQUIRED_GROUP='CN=Desktop Users,OU=Groups,DC=example,DC=com' \
  GATEWAY_LDAP_ADMIN_GROUP='CN=Desktop Admins,OU=Groups,DC=example,DC=com' \
  GATEWAY_LDAP_CA=/etc/devbox-kvm/directory-ca.pem
```

| Make variable | Default |
| --- | --- |
| `GATEWAY_LDAP_LOCAL` | `1`; use `0` to skip the bundled server |
| `LDAP_PORT` | `1636` |
| `GATEWAY_LDAP_URL` | `ldaps://127.0.0.1:$(LDAP_PORT)` |
| `GATEWAY_LDAP_BASE_DN` | `dc=glauth,dc=com` |
| `GATEWAY_LDAP_DOMAIN` | `example.com` |
| `GATEWAY_LDAP_FILTER` | `(mail=%s)` |
| `GATEWAY_LDAP_USERNAME_ATTRIBUTE` | `mail` |
| `GATEWAY_LDAP_REQUIRED_GROUP` | `ou=team10_r,ou=groups,dc=glauth,dc=com` |
| `GATEWAY_LDAP_ADMIN_GROUP` | `devbox-admins` |
| `GATEWAY_LDAP_CA` | `./testldap/.tls/cert.pem` |

Set `GATEWAY_LDAP_CA=` to use the system certificate authorities. Set
`GATEWAY_LDAP_REQUIRED_GROUP=` to omit the login group restriction. Set
`GATEWAY_LDAP_ADMIN_GROUP=` to disable administrator access. The admin setting
accepts a bare group name or a full group DN and maps to `serve -ldap-admin-group`.
It checks direct, case-insensitive `memberOf` membership at login and does not
expand nested groups. The required login group still applies to administrators;
changes to group membership take effect on the next login. The username
`administrator` alone never grants a role.

`ldap://` connections use StartTLS; `ldaps://` connections negotiate TLS immediately. The canonical
username attribute must contain a single supported lowercase Linux account name,
or an email address matching `GATEWAY_LDAP_DOMAIN`; its local part identifies the
account and owns its workspaces.

`make gateway` has no generated-auth-file fallback. The optional
`devbox-kvm serve -auth-file` CLI mode remains available for explicit loopback-only
development setups and never grants administrator roles. Old
`.devbox-kvm-gateway/auth.json` and `.devbox-kvm-gateway/auth.json.credentials.json`
files are not used by the LDAP setup, and existing `developer` workspaces are not
reassigned to an LDAP account.

Image uploads default to 20 GiB and 30 minutes. Override them with
`GATEWAY_ARGS="-image-upload-limit 42949672960 -image-upload-timeout 1h"`.
The size limit also applies to normalized images. Compressed QCOW2 imports use
the built-in Go normalizer and need no external conversion tool. See
[the gateway documentation](../docs/gateway.md) for image requirements and
storage behavior.
