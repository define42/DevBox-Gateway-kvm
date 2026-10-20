# Contributing to DevBox-Gateway-kvm

## Start here

Use x86-64 Linux, Go 1.26.0 or newer, Git, and Make. Building the default software
H.264 encoder needs no C compiler. These commands build the application and run
focused tests without booting a guest:

```bash
git clone https://github.com/define42/DevBox-Gateway-kvm.git
cd DevBox-Gateway-kvm
CGO_ENABLED=0 go build -o gokvm .
./gokvm serve -help
./gokvm boot -help
CGO_ENABLED=0 go test -short ./internal/config ./internal/flag \
  ./internal/gateway ./internal/cloudinit
```

Some tests open loopback TCP listeners. Running guests needs `/dev/kvm`; the
full test suite also needs networking, firmware, and image tools. See the
[development guide](docs/development.md) for prerequisites, focused race checks,
dashboard tests, optional integrations, and the code layout.

## Making a change

- Keep the change focused and add a regression test for behavior changes.
- Format changed Go files with `gofmt`. Document exported APIs and non-obvious
  ownership, lifecycle, and error constraints.
- Update the relevant [user guide](README.md#commands-and-documentation) and CLI help when
  changing commands, defaults, configuration, or guest requirements.
- Run the relevant focused tests and `make golangci`. When the host prerequisites
  are available, run `make test` for the full suite, including QCOW2/UEFI and audio
  checks and the 80% repository-wide statement coverage gate. `make coverage`
  runs the coverage suite alone. Do not treat skipped hardware checks as passes.
- In the pull request, explain the user-visible change and list the checks run,
  their results, and any checks you could not run.

`make generate` refreshes generated enum strings; include the generated changes
when modifying their source definitions. CI and release behavior are described in
the [development guide](docs/development.md#ci-coverage) and
[README](README.md#contributing).

## Reporting a problem

Include the commit or release, Go version, host OS/architecture, exact command,
expected and actual behavior, and a minimal reproduction. For VM problems, include
the boot mode, firmware, guest image/version, and relevant device or encoder
settings.

Use disposable disks and private test state. Do not attach credentials, session
or RDP routing tokens, TLS private keys, or live gateway state/disks to issues or
pull requests. Redact sensitive values from commands and logs before sharing them.
