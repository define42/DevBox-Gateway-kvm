# VM worker seccomp profile

`seccomp-vm.json` is the Docker default profile from
[moby/profiles seccomp/v0.2.3](https://github.com/moby/profiles/blob/seccomp/v0.2.3/seccomp/default.json),
the version embedded in the tested Docker Engine 29.7.2.
It is licensed under Apache-2.0; see `seccomp-vm.LICENSE`.

Upstream SHA-256:
`536529b665dd0972c37bfb569f5d4ac8a53592e7b00752bc39ff063ca9864c74`.

The only modification replaces the `clone3` ENOSYS rule with `SCMP_ACT_ALLOW`
and removes that rule's `errnoRet`. All other rules remain identical. Go's
`SysProcAttr.UseCgroupFD` requires `clone3(CLONE_INTO_CGROUP)` to place the worker
inside its capped cgroup before executing guest code.

Seccomp cannot inspect the pointed-to `clone_args` structure, so it cannot limit
this permission to that particular flag. Kernel capability, namespace, cgroup
permission, and LSM checks still apply. The profile does not grant capabilities
or disable the remaining syscall filter. Review this pinned profile when
upgrading Docker; it does not automatically inherit future default-policy changes.
