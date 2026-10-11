# Local LDAP directory

This directory brings the DevBox-Gateway demo LDAP users and groups into
DevBox-Gateway-kvm.
[`default-config.cfg`](default-config.cfg) is copied unchanged from
[DevBox-Gateway commit `e4fbf205`](https://github.com/define42/DevBox-Gateway/blob/e4fbf205adb7663167190f1082c41ff81681d3d8/testldap/default-config.cfg),
with its [MIT license](LICENSE). Compose runs GLAuth with a pinned image digest.
The server is published only on `127.0.0.1:1636` and requires TLS.

Install Docker with the Compose plugin and OpenSSL, then run from the repository
root:

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
to each VM’s owner, even for administrators. The configured default image cannot
be removed; existing VMs retain independent disks if an uploaded image is removed.
New guests use the authenticated username and password at their graphical login.
Existing guests retain their provisioned passwords and owners.

`generate-cert.sh` creates a local certificate and private key in the ignored
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
  GATEWAY_LDAP_CA=/etc/gokvm/directory-ca.pem
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
`gokvm serve -auth-file` CLI mode remains available for explicit loopback-only
development setups and never grants administrator roles. Old
`.gokvm-gateway/auth.json` and `.gokvm-gateway/auth.json.credentials.json` files are not used by the LDAP setup, and existing `developer` workspaces are not
reassigned to an LDAP account.

Image uploads default to 20 GiB and 30 minutes. Override them with
`GATEWAY_ARGS="-image-upload-limit 42949672960 -image-upload-timeout 1h"`.
The size limit also applies to normalized images. Install `qemu-utils` to import
compressed QCOW2 images; `qemu-img` is used only for conversion, and gokvm runs
all VMs. See [the gateway documentation](../docs/gateway.md) for
image requirements and storage behavior.
