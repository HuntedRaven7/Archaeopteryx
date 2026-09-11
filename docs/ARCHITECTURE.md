# Archaeopteryx architecture

Archaeopteryx is a Talos-like control plane for Microraptor factory images: a
node-side daemon (`apxd`) exposing a gRPC API over mutual TLS, with a CLI
(`apxctl`) mirroring the key Talos workflows — but built around Microraptor's
actual stack (`systemd-sysupdate` A/B slots, `systemd-sysext` for k0s, kured
reboots).

## Components

| Layer | Package | Responsibility |
|---|---|---|
| API contract | `api/apx/v1` | protobuf `MachineService` + `Events` (see [gRPC surface](#grpc-surface)) |
| Daemon | `cmd/apxd` → `internal/apid` | PKI, reloadable mTLS creds, gRPC server, maintenance mode, handlers |
| Host mechanics | `internal/machine` | systemd/journal, sysupdate, bootctl, sysext/k0s, lifecycle, events |
| Config/PKI | `internal/config` | `apxconfig` machine config, `gen config`, client bundle, certificate generation |
| CLI | `internal/cli` | cobra command tree, bundle resolution, streaming output |
| Client | `pkg/client`, `pkg/version` | gRPC dial helper, build info |

## Certificate model

Talos-style, but offline-first: no on-node CA.

1. `apxctl gen config <cluster> <endpoint>` generates a CA plus server and
   admin client identities **locally** and emits:
   - `apxconfig.yaml` — the machine config embedding the CA and the server
     identity the node should serve;
   - `apxctl.yaml` — a client bundle (`contexts[<name>] → {endpoint, ca, cert,
     key}`, selectable with `--context`, default first);
   - `ca.crt` / `admin.crt` / `admin.key` for the outgoing admin.
2. An unconfigured node runs in **maintenance mode**: `apxd` self-generates a
   throwaway bootstrap PKI under its state dir (`/var/lib/archaeopteryx`) and
   prints a one-per-boot onboarding token.
3. `apxctl apply-config <file> --maintenance-token <token>` (connected with the
   bootstrap admin identity) installs the config: hostname, CA, server cert/key.
   It deletes the bootstrap **CA key** and the onboarding token, exits
   maintenance mode, and reloads its creds per handshake
   (`internal/apid/creds.go`).
4. From then on clients use the bundle; the old bootstrap identity is rejected
   (fresh connections re-handshake against the new CA).

## gRPC surface

`MachineService` (mTLS, port 50000) — trimmed from Talos to a k0s single-node
factory OS:

| RPC | Kind | Notes |
|---|---|---|
| `Version`, `GetStatus` | unary | build info; os-release/kernel/uptime/boot id/slot + k0s state |
| `Bootstrap` | unary | locate k0s sysext → `systemd-sysext merge` → enable+start `k0scontroller.service` → poll local apiserver until Ready |
| `Kubeconfig` | unary | `k0s kubeconfig admin`; CLI `--merge`/`--file` rewriting under an `apx-<host>` context |
| `Logs` | server-stream | `journalctl -o json` with `-f/-u/-n/--since`; child killed when the client disconnects |
| `ListServices` / `ServiceAction` | unary | `systemctl list-units -o json` + batched `systemctl show`; validated unit names |
| `Reboot` / `Shutdown` | unary | `systemctl reboot|poweroff` (`--mode=poweroff`) |
| `Reset` | unary | stop+disable k0s → `k0s reset` → wipe `/var/lib/k0s` (+ config/TLS with `--wipe`, returning to maintenance mode) → sysext unmerge → reboot |
| `ApplyConfig` / `GetConfig` | unary | maintenance-mode onboarding; redacted dump |
| `Update` | server-stream | plan progress lines (see below) |
| `Rollback` | unary | `bootctl set-oneshot <previous-UKI>` + optional reboot |
| `Events` | server-stream | journal-derived typed events (k0s/update/machine) |

## Update model

Backend is `systemd-sysupdate` (v251+, `features`/`pending`/`update` verbs),
not bootc. `internal/machine/sysupdate.go` parses `features` output into
`Component`/`Transfer` descriptors; the latest version is discovered with a
**HEAD request to the GitHub `releases/latest/download/<asset>` URL** — the
`Location` redirect header carries the tag, so no GitHub API is needed.

`internal/machine/upgrade.go` classifies the plan:

- **os only** → `systemd-sysupdate update --component=<os> --verify=yes` →
  REBOOT REQUIRED (new A/B slot installed, not running);
- **k0s only** → `systemd-sysupdate update --component=k0s` → `systemd-sysext
  merge` → NO reboot;
- **os + k0s** → stage both; the reboot covers k0s.

Reboot strategy comes from `machineConfig.update.rebootStrategy`:
`manual` (operator confirms), `staged` (leave `/run/reboot-required`; kured
drains + reboots), `direct` (reboot immediately after staging).

`rollback` picks the other `/efi/EFI/Linux/*.efi` entry and runs `bootctl
set-oneshot`, complementing systemd-boot boot counting.

## Events

`apxctl events` maps a followed journal stream to `apx.v1.Event` records,
keeping records from curated units (k0s, sysupdate/sysext, the apx daemon, the
kernel) and anything at syslog priority `err` or worse. Each event carries
`id`, `timestamp_ns`, `type` (k0s/update/machine), `message`, and metadata
(unit/ident/hostname).

## Packaging

`packaging/microraptor/` + `just image-tree` stage `dist/rootfs/` — static
(`CGO_ENABLED=0`) `apxd`/`apxctl`, the hardened `apxd.service`, and the
`zz-enable-apxd.preset` — in the DDI target layout. The Microraptor repo
consumes it via an `os-apx` copy element in `os-stack.bst`; `/var/lib/archaeopteryx`
persistence is already covered by `var.mount`, and sysupdate verification keys
stay in the image.

Smoke coverage: `just smoke` (loopback onboarding E2E, runs on any host) and
`hack/qemu-smoke.sh` (full VM against a real Microraptor DDI, gated on
`MICRORAPTOR_IMAGE`).

## Code conventions

- **Static binaries**, no external runtime deps beyond the base DDI.
- Every host command (`systemctl`, `journalctl`, `systemd-sysupdate`,
  `bootctl`, `k0s`, ...) is behind a package-level seam
  (e.g. `execSystemctlLifecycle`, `execSysupdateFeatures`) so unit tests can
  record/swap argv instead of executing real tools.
- Tests are hermetic: parsers/formatting/classification tested with fixtures;
  live integration is covered by `hack/smoke.sh` and the QEMU harness.
- `just check` = fmt, vet, golangci-lint, unit tests, build.