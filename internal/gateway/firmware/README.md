# Bundled OVMF firmware

The gateway embeds this matching plain, non-SMM x86-64 firmware pair using
Go `embed`. A normal `go build` needs no host firmware installation or download.
The CODE image is 3,653,632 bytes and the pristine VARS template is 540,672 bytes;
together they form a 4 MiB flash image.

These unmodified files were copied from Ubuntu's `ovmf` package version
`2024.02-2ubuntu0.10` (`Architecture: all`). Upstream is
[TianoCore EDK II](https://github.com/tianocore/edk2); the package source is
[Ubuntu edk2](https://launchpad.net/ubuntu/+source/edk2/2024.02-2ubuntu0.10).
`LICENSE` is the corresponding `/usr/share/doc/ovmf/copyright`, preserved
verbatim. It is also embedded and extracted with the firmware, and included
in the Docker image's attribution directory.

SHA-256 checksums:

```text
ad23261d43116abc3747d59a7837fbf578c9586a8993f4eeba064cd07eccb083  OVMF_CODE_4M.fd
5d2ac383371b408398accee7ec27c8c09ea5b74a0de0ceea6513388b15be5d1e  OVMF_VARS_4M.fd
```

To update, replace both files from the same trusted OVMF package, preserve its
copyright notices in `LICENSE`, and record the new version and checksums here.
Use the plain CODE image, not the Secure Boot/SMM variant. Keep the variable
store format compatible with existing VMs: gateway startup refreshes only the
bundled templates under `DATA_ROOT_DIR/firmware`, never existing VM `vars.fd`
files. Run the gateway firmware tests and `make test-qcow2` before shipping a
new firmware pair. The gateway always uses the bundled pair, so updating its
firmware requires rebuilding the binary.
