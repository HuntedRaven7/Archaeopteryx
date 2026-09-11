---
name: archaeopteryx
description: Agent skill for developing the Archaeopteryx project — a Talos-like gRPC control plane (apxd daemon + apxctl CLI) for Microraptor k0s single-node factory images. Use when asked to add or fix features, run or extend the update/rollback/bootstrap/kubeconfig/logs/lifecycle commands, work on the proto/API contract, write or run tests, update PLAN.md/README.md milestones, or run just/build/smoke recipes.
---

# Archaeopteryx development skill

Archaeopteryx is a series of milestones implementing a Talos-like node control
plane. This skill tells an agent everything it needs to work on the repo
efficiently and correctly: the current state, the invariants, the environment
constraints, the test strategy, and the exact workflow expected of a helper.

## Repo at a glance

- `api/apx/v1/apx.proto` — the full gRPC contract (`MachineService` + `Events`).
  Generated code is already committed (`apx.pb.go`, `apx_grpc.pb.go`); edit the
  `.proto` and run `just proto` when changing the contract.
- `cmd/apxd` — daemon: PKI + reloadable mTLS creds + gRPC server.
- `cmd/apxctl` — cobra CLI root (`internal/cli` holds the command tree).
- `internal/apid` — handlers (`machine.go`, one method set per RPC) + `server.go`.
- `internal/machine` — all host mechanics, one file per concern:
  - `status.go` (os-release/uname, `runOutput` helper), `hostname.go`
  - `systemd.go`, `journal.go` (journalctl JSON stream)
  - `sysupdate.go` (features/pending parsers, version compare, latest-version
    discovery), `upgrade.go` (plan builder, update flow, rollback)
  - `k0s.go` (bootstrap: sysext merge + controller), `kubeconfig.go`,
    `kubeconfig_merge.go`
  - `lifecycle.go` (reboot/shutdown/reset), `events.go` (journal-derived events)
- `internal/config` — `apxconfig` machine config, PKI/certs, client bundle.
- `pkg/client`, `pkg/version` — gRPC dial helper, ldflag build info.
- `packaging/microraptor/` — systemd unit + preset + DDI layout notes.
- `hack/` — `gen-proto.sh`, `smoke.sh` (loopback E2E), `qemu-smoke.sh`.
- `Justfile` — the developer interface (`just check`, `just smoke`, ...).
- `docs/ARCHITECTURE.md` — protocol, cert model, update flow prose.

## Current state

- Milestones 1–7 are ✅ and committed by the user; milestone 8 (docs + gates)
  is being wrapped up. Each completed milestone flipped its `PLAN.md` bullet to
  ✅ and updated the README command sections.
- **Never commit unless the user explicitly asks.** The user commits each
  milestone themselves.

## Hard environment constraints (this sandbox)

The build/test sandbox is NOT the target OS and NOT root:

- It runs a real systemd (systemd 257) with journald, so `systemctl`
  read/query, `journalctl`, and `hostnamectl` work.
- Mutating actions fail for lack of privileges: `systemctl reboot`,
  `service start/restart`, `apply-config` hostname changes, and any write to
  `/var/lib/archaeopteryx` error out (exit status 3 / "Access denied"). Tests
  and smoke must stub these or accept the graceful failure.
- `systemd-sysupdate` is NOT installed and `k0s` is NOT installed. Anything
  that shells out to them must be tested via **fixtures and seams**, never live.
- A wrapper shell's command line matches `pkill -f <pattern>`: kill daemons
  with `pkill -x apxd` (exact name). Background the daemon fully detached:
  `(setsid ./dist/apxd ... > log 2>&1 < /dev/null &)` because the bash tool
  kills background jobs when a command times out.

## The seam pattern (use this everywhere)

Every host command is a package-level func var so tests can replace it:

```go
var execSystemctlLifecycle = func(args ...string) error { _, err := runOutput("systemctl", args...); return err }
```

Tests swap it with a recorder and asserts on argv. Examples: `m3_test.go`
(systemd/journal), `m4_test.go` (k0s with a fake `k0s` in PATH), `m5_test.go`
(features fixtures, version compare, slot pick), `m6_test.go` (lifecycle
sequencing via `recordingExec`). Follow the same shape for new commands.

## Adding a feature end to end

1. `api/apx/v1/apx.proto`: add the message + RPC → `just proto`.
2. `internal/machine/`: implement the mechanics with seams (see above).
3. `internal/apid/machine.go`: add the handler, mapping machine results to API
   types and errors to `status.Error(codes.*, ...)`. Note generated streaming
   signatures: server-side streams are
   `grpc.ServerStreamingServer[T]`; clients use the `UpdateResponse` oneof
   pattern (plan/progress/error).
4. `internal/cli/<cmd>.go`: cobra command; register in `root.go`
   `AddCommand(...)`. Streams use `signal.NotifyContext` + Recv loop; tables
   print with `tabwriter`. Use `newClient()` (`resolvedOptions()` merges global
   flags with the bundle; `--context` selects a named bundle context).
5. Tests: hermetic unit tests in `internal/machine/mN_test.go` (fixture
   parsers, recorded exec flows), mirror any wire mapping with the existing
   `startTestServer(t)`/`newTestClient` helpers in
   `internal/apid/integration_test.go`.
6. Verify: `just check` (fmt, vet, golangci-lint, test, build) then
   `just smoke` for a live round-trip.
7. Documentation: flip the milestone bullet in `PLAN.md` to ✅ with behavior
   notes, add usage examples to `README.md`, and if behavior changed, refresh
   `docs/ARCHITECTURE.md`.

## CLI gotchas already solved

- Cobra's multi-word `Use` treats the first word as the command name, so a
  `gen config <a> <b>` command must be modeled as a `gen` parent with a
  `config` child (positional args otherwise shift by one).
- gRPC connections are persistent: after `apply-config` swaps the CA, an
  already-open client connection is still accepted until a fresh dial. Tests of
  "old identity rejected" must create a NEW client connection.
- `runOutput(name, args...)` wraps errors with captured stderr
  (`fmt.Errorf("%w: %s", err, s)`) — expect that in error messages.

## Commands cheat-sheet

```sh
just check       # fmt, vet, lint (skips if golangci-lint missing), test, build
just test ./internal/machine     # run one package
just proto       # regenerate protobuf/gRPC code (buf)
just smoke       # loopback onboarding E2E (green on this host)
just cross       # static linux/amd64 + arm64
just image-tree  # stage dist/rootfs (DDI drop-in)
just qemu-smoke  # VM smoke; needs MICRORAPTOR_IMAGE (Microraptor repo)
```

Code style: gofmt clean, `go vet ./...`, staticcheck; no comments in code
unless the user asks; keep responses short.