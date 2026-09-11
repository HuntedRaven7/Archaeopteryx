# Archaeopteryx — Talos-like API + `apxctl` for Microraptor

A lean Go control plane for a Microraptor node, mirroring the shape of Talos: a node-side
daemon exposing a gRPC API over mTLS, with streaming logs/events and orchestrated
upgrade/reboot flows — but built around Microraptor's actual stack.

- **Updates:** `systemd-sysupdate` with GPT A/B slots (whole-DDI, UKI) plus an
  independently-pinned k0s sysext, verified against GPG-signed `SHA256SUMS` from GitHub
  Releases.
- **k0s:** delivered as a `systemd-sysext` overlay; `k0scontroller.service` runs
  `k0s controller --enable-worker --single --disable-components=helm,autopilot`.
- **Base:** distroless FSDK DDI, SSH for admin with root key auth, kured reboot hook
  (`/run/reboot-required`).

## Goals

- A clean, minimal, Talos-like machine API for Microraptor nodes.
- A `apxctl` CLI covering the key Talos workflows: status, version, machine info,
  bootstrap + kubeconfig, update + rollback, logs + services, reboot / shutdown / reset,
  apply-config + machine config, and an events stream.
- An update command that **auto-determines how to proceed with an update and reboot**.

## Architecture

```
┌────────────┐  gRPC mTLS (port 50000)  ┌─────────────────────────────┐
│  apxctl    │ ───────────────────────▶ │  apxd (node daemon, root)    │
│  (cobra)   │ ◀─────────────────────── │  MachineService, Events      │
└────────────┘   stream logs/events     └─────────────────────────────┘
                                        │  systemd D-Bus  (start/stop/reboot/journal)
                                        │  systemd-sysupdate (features/pending/update)
                                        │  systemd-boot    (A/B slots, rollback)
                                        │  k0s sysext      (stage/merge, controller, kubeconfig)
                                        │  os-release/uname (status)
                                        └─────────────────────────────┘
```

### Cert model (Talos-style)

- `apxctl gen config <cluster> <endpoint>` generates a CA + server/client certs locally
  and emits:
  1. an `apxconfig` machine-config embedding the CA, and
  2. a `talosconfig`-style client bundle.
- Unconfigured nodes run in **maintenance mode** (ephemeral per-boot token shown on the
  console) so `apxctl apply-config` can bootstrap them — mirrors Talos's MaintenanceService.
- Server verifies client certs against the CA from `apxconfig`; client verifies the server
  against the same CA plus the endpoint.

### Version model

- OS `@v` = FSDK point release, sourced from `IMAGE_VERSION` in os-release.
- k0s is independently pinned (its own sysext version axis).
- Update candidacy is discovered via `systemd-sysupdate features` (v251+) — no
  GitHub-API parsing needed.

## Repository layout

```
github.com/HuntedRaven7/Archaeopteryx
├── go.mod
├── cmd/
│   ├── apxctl/            # CLI entry point
│   └── apxd/              # daemon entry point
├── api/
│   └── apx/v1/
│       ├── apx.proto      # protobuf definitions
│       ├── apx.pb.go      # generated
│       └── apx_grpc.pb.go # generated
├── pkg/
│   ├── client/            # gRPC client shared by apxctl
│   └── version/           # build information
├── internal/
│   ├── apid/              # gRPC server, mTLS, maintenance mode, event bus
│   │   ├── server.go
│   │   ├── maintenance.go
│   │   ├── machine.go     # MachineService implementation
│   │   ├── events.go      # event bus + stream subscribers
│   │   └── resources.go   # os-release, uname, slot info
│   ├── machine/
│   │   ├── systemd.go     # systemd D-Bus operations
│   │   ├── journal.go     # journald reader
│   │   ├── sysupdate.go   # features / pending / update orchestration
│   │   ├── bootloader.go  # bootctl slots and rollback
│   │   ├── k0s.go         # sysext stage/merge, controller, kubeconfig
│   │   └── upgrade.go     # upgrade plan builder + reboot policy
│   ├── config/            # apxconfig decode/validate + TLS generation
│   └── cli/               # cobra command tree
├── hack/                  # buf codegen config/scripts
├── Justfile               # just fmt / lint / test / build / proto
└── README.md
```

Static binaries (`CGO_ENABLED=0`). Journald read via streaming `journalctl -o json -f`,
with an sdjournal fallback if the DDI lacks `journalctl`.

## gRPC surface

`MachineService` + `Events` streaming — trimmed from Talos to what a k0s single-node
factory OS needs:

| RPC | Description |
|---|---|
| `GetStatus` | os-release, kernel, uptime, boot ID, slot info, k0s state |
| `Version` | server build/version info |
| `Bootstrap` | stage+merge k0s sysext, start controller, wait node Ready |
| `Kubeconfig` (stream) | `k0s kubeconfig admin` |
| `Logs` (stream) | journald by unit or all, follow |
| `ListServices` | systemd unit states |
| `ServiceAction` | start / stop / restart / reload |
| `Reboot` | graceful (stop k0s) via login1 |
| `Shutdown` | graceful via login1 |
| `Reset` | stop k0s, wipe /var state, optional `--wipe`, reboot |
| `ApplyConfig` | write machine config, optional reboot |
| `GenerateClientConfig` | onboarding cert generation |
| `Update` (stream) | check / plan / stage progress, `--reboot` |
| `Rollback` (stream) | `bootctl set-oneshot` + optional reboot |
| `Events` (stream) | machine + update + k0s events |

## `apxctl` command tree

```
apxctl
├── gen config <cluster> <endpoint>   # generate CA + certs + apxconfig + client bundle
├── config merge | get | context
├── apply-config <file> [--reboot]
├── update    [--check] [--staged] [--reboot] [--component=os|k0s|all]
├── rollback  [--reboot]
├── status
├── version
├── bootstrap
├── kubeconfig [--merge] [--file]
├── logs      [-f] [-u=unit] [--tail=N] [--since=...]
├── services  [<unit>]
├── service <start|stop|restart|reload> <unit>
├── reboot    [--mode=graceful|poweroff]
├── shutdown
├── reset     [--wipe]
└── events
```

## Update command — the auto-determine core

```
apxctl update [--check] [--staged] [--reboot] [--component=os|k0s|all]

 1. apxd builds the upgrade plan:
    - os  : systemd-sysupdate features            (root A/B + UKI + DDI staging)
    - k0s : systemd-sysupdate --component=k0s features
    - also: systemd-sysupdate pending             (reboot already owed?)

 2. Classify the plan:
    - none    → "up to date", exit 0
    - os only → stage via `systemd-sysupdate update [--verify=yes]`
                → REBOOT REQUIRED (new slot installed, not running)
    - k0s only→ `systemd-sysupdate --component=k0s update`
                → `systemd-sysext merge` (+ optional k0scontroller restart)
                → NO reboot needed
    - os + k0s→ stage both; reboot covers k0s

 3. Reboot policy (config `update.rebootStrategy`):
    - manual  → prompt on tty; non-tty defaults to --staged
    - staged  → apply, leave reboot-required (honors the existing kured
                /run/reboot-required hook: kured drains & reboots)
    - direct  → cordon+drain node via Kubernetes API (if k0s healthy)
                → systemctl stop k0scontroller → login1 Reboot
```

`rollback`: resolve the previous slot from `systemd-boot` entries → `bootctl
set-oneshot <prev>` → optional `--reboot` (complements systemd-boot boot counting).

## Machine config (`/var/lib/archaeopteryx/config.yaml`)

Minimal by design — no SSH section (Microraptor owns SSH) and no duplicate k0s flags
(owned by `k0scontroller.service`).

```yaml
version: v1alpha1
hostname: node-01
api:
  endpoints:
    - https://10.0.0.5:50000
  ca: <pem>
  cert: <pem>      # optional server cert (self-generated in maintenance mode)
  key: <pem>
maintenance:
  token: <optional bootstrap gate>
update:
  channel: stable          # maps to a GitHub release channel
  rebootStrategy: staged   # manual | staged | direct
  autoStage: true
  verify: required
  restartK0sOnSysext: false
k0s:
  enableWorker: true
  single: true
  disableComponents: [helm, autopilot]
  extraFlags: []
```

## Bootstrap / reset flows

- **bootstrap:** stage + merge `k0s.raw` (reuses `k0s-first-boot` logic) → enable + start
  `k0scontroller.service` → wait for node `Ready` via the local kube-apiserver.
- **kubeconfig:** run `k0s kubeconfig admin`, stream back; `--merge` into KUBECONFIG.
- **reset:** stop k0s, wipe `/var` state; `--wipe` for a full factory reset, then reboot.

## Microraptor integration (separate repository, follow-up)

- `elements/microraptor/os-apx.bst` → include in `os-stack.bst`.
- `files/os/systemd/system/apxd.service`.
- preset `zz-enable-apxd.preset`.
- `/var/lib/archaeopteryx` persisted (covered by existing `var.mount`).
- sysupdate keys / verification reused as-is.

## Milestones

1. **Scaffold** — proto + codegen, `apxd` skeleton, `apxctl version/status` round-trip
   (local + mTLS).
2. **Auth / onboarding** — ✅ CA management, `gen config`, `apply-config`, maintenance mode.
   - `apxctl gen config <cluster> <endpoint>` (offline) → `apxconfig.yaml` + `apxctl.yaml`
     bundle + CA/admin identity in `--output-dir`.
   - Unconfigured daemon serves reloadable mTLS creds backed by a self-generated bootstrap
     PKI and prints one per-boot onboarding token (maintenance mode).
   - `apxctl apply-config <file> [--maintenance-token]` swaps hostname, CA, and server
     identity and deletes the bootstrap CA key + token (config stays at
     `/var/lib/archaeopteryx/config.yaml`).
   - Post-apply, old bootstrap clients are rejected; operator connects via
     `apxctl --bundle apxctl.yaml`. `config get` returns the config redacted (secrets masked).
3. **systemd / journal** — ✅ `logs`, `services`, `service` action.
   - `apxctl logs [-f] [-u unit] [--tail=N] [--since=...]`: daemon streams
     `journalctl -o json` (killed when the client disconnects), rendering lines as
     `<ts> <host> <ident>[<pid>]: <message>`.
   - `apxctl services`: `systemctl list-units --type=service -o json`, enriched with
     `ActiveEnterTimestampMonotonic` via one batched `systemctl show`.
   - `apxctl service <start|stop|restart|reload> <unit>`: unit names validated
     before ever reaching systemctl.
4. **k0s** — ✅ `bootstrap`, `kubeconfig`, status wiring.
   - `apxctl bootstrap`: locates the k0s sysext image (`/var/lib/extensions/k0s.raw`
     or `/var/lib/k0s/k0s.raw`), runs `systemd-sysext merge` when not merged,
     `systemctl enable --now k0scontroller.service`, then polls the local
     kube-apiserver (via the k0s `admin.conf`) until the node reports Ready.
   - `apxctl kubeconfig [--merge] [--file]`: streams `k0s kubeconfig admin`;
     `--merge` rewrites the doc under an `apx-<endpoint-host>` identity and merges
     it into `~/.kube/config` (or `KUBECONFIG`), idempotently replacing prior
     `apx-<host>` entries and switching `current-context`.
   - Status wiring: `GetStatus` already surfaces sysext merge + controller state.
5. **Update/rollback engine** — ✅ `update`, `rollback`, reboot policy.
   - `internal/machine/sysupdate.go`: `Features()`/`RebootOwed()` wrap `systemd-sysupdate
     features|pending`, features parsed into `Component`/`Transfer` sections; latest-version
     discovery via the GitHub `latest/download` HEAD redirect (`Location` header), no API.
   - `internal/machine/upgrade.go`: `BuildPlan` classifies components (current vs latest,
     reboot-required unless k0s), `Update` stages via `systemd-sysupdate update
     --component=<name> --verify=yes`, merges the k0s sysext, then applies the configured
     reboot strategy (manual|staged|direct); `Rollback` = previous UKI slot → `systemd-boot
     set-oneshot`, optional reboot. All exec seams are swappable for tests.
   - CLI: `apxctl update [--check] [--component=os|k0s|all] [--reboot]` streams plan +
     progress; `apxctl rollback [--reboot]`. Unit tests: features/pending parsers, version
     comparison (dotted+`v`/k0s styles), plan classification, slot picking.
6. **Lifecycle** — ✅ `reboot`, `shutdown`, `reset`, `events`, context polish.
   - `internal/machine/lifecycle.go`: `Reboot` (graceful/poweroff), `Shutdown`, `Reset` —
     stop+disable k0s → `k0s reset` → wipe state dirs (`/var/lib/k0s`, and the apx config +
     TLS identity with `--wipe`, returning to maintenance mode) → `systemd-sysext unmerge`
     → reboot. All execs behind seams.
   - `internal/machine/events.go`: `StreamEvents` tails the journal via
     `StreamJournal`, filtering to curated units (k0s/sysupdate/sysext/apxd/kernel) plus
     anything at `ELOG_PRI_ERR` or worse, mapped to typed `NodeEvent`s (k0s/update/machine).
   - CLI: `apxctl reboot [--mode=graceful|poweroff]`, `apxctl shutdown`, `apxctl reset
     [--wipe]`, `apxctl events`. `--context <name>` selects a named bundle context
     (default: first). Live smoke verified the full RPC path against real journald +
     systemd.
7. **Packaging** — Microraptor DDI integration + QEMU smoke test
   (`just show-me-the-future`-style).
8. **Docs + gates** — README, `just check` (fmt, vet, test, golangci-lint).

## Verification

- `just fmt` → `just vet` → `just test` (unit: config, plan builder, slot parsing, version
  compare, lifecycle cmd sequencing, event predicate; golden gRPC tests on loopback)
  → `just build`.
- Cross-compile `linux/amd64` and `linux/arm64`.
- QEMU smoke test of installer → first boot → `apxctl bootstrap` → `apxctl update
  --check`.