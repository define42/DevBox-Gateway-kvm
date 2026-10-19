# Storage capacity responsibility

## Accepted deployment boundary

This is a fresh deployment. Docker Compose uses the Compose-only
`GATEWAY_STORAGE_PATH` variable to select the host state directory. Its default
is `/srv/devbox-storage`, which is bind-mounted as `/var/lib/gokvm` in the
gateway container.

The mount contains the entire gateway state tree, not only VM disks. This
includes managed VM disks and firmware variables, QCOW2 catalog images, upload
and import staging, seeds, VM and gateway certificates and keys, ACME state,
metadata, temporary files, and the manager lock.

`GATEWAY_STORAGE_PATH` does not change native execution. The native
`DATA_ROOT_DIR` default remains `.gokvm-gateway`, and operators may continue to
override it with `DATA_ROOT_DIR` or `-state`. Inside the Docker image,
`DATA_ROOT_DIR` defaults to `/var/lib/gokvm`. Keep that value for the shipped
Compose mount; changing it also requires updating the mount target, otherwise
the gateway may write outside the persistent host directory.

No migration, deletion, filesystem creation, mount configuration, quota setup,
or other host provisioning is performed. The administrator must prepare the
fresh host path with sufficient capacity and private ownership and permissions
for the host identity mapped to container UID/GID 10001. See
[Docker setup](docker.md#start-the-gateway-and-ldap-with-compose) for preparation
and user-namespace considerations. No filesystem type is required by the product.

## Product limit

`VM_DISK_MAX_SIZE` defaults to `500GB` and limits the reported virtual capacity
of each template and managed VM disk. It rejects oversized imports and disks;
it does not preallocate that capacity, limit allocated host blocks, impose an
aggregate quota, or reserve free space. Sparse QCOW2 images can grow as guests
write, and imports may temporarily require space for both uploaded and
normalized data.

Storage-specific HTTP errors for recognized `ENOSPC` and `EDQUOT` failures do
not contain filesystem exhaustion. The gateway does not provision or verify
filesystem quotas, block or inode ceilings, a protected
free-space floor, or per-VM/per-owner isolation. Selecting
`/srv/devbox-storage` changes where state is persisted; it does not add those
controls.

## Administrator responsibility

The administrator owns the shared-storage policy across all users and gateway
state, including:

- sizing and monitoring available blocks and inodes;
- deciding whether physical or thin-provisioned capacity may be overcommitted;
- configuring any filesystem, volume, user, group, or project quotas;
- maintaining any protected recovery reserve;
- accounting for VM growth, catalog images, concurrent creation, import
  staging, image-format overhead, metadata, and temporary files;
- alerting and recovery when space or inodes run low; and
- backups, restore testing, and retention.

If the administrator does not provide host-side isolation, one VM, owner, or
administrator import can exhaust the shared filesystem and disrupt unrelated
gateway writes. H04 therefore remains an accepted, operator-managed residual
risk rather than a product-enforced fix.

This storage responsibility boundary does not alter the independent H05 cgroup
memory admission and cleanup behavior described in
[Gateway configuration](configuration.md#storage-and-desktops).
