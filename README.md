# Archaeopteryx

A Talos-like control plane for [Microraptor](https://github.com/HuntedRaven7/Microraptor):
a node-side gRPC API daemon (`apxd`) and a CLI (`apxctl`) for k0s single-node
factory images built on `systemd-sysupdate` A/B slots.

See [PLAN.md](PLAN.md) for the architecture and the implementation roadmap and
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for a prose deep-dive of the
protocol, certificate model, and update flow.

## Repository layout

- `api/apx/v1/apx.proto` — the gRPC contract (`MachineService`).
- `cmd/apxd` — the node daemon: PKI + mTLS listener + API implementation.
- `cmd/apxctl` — the CLI (`status`, `version`, ...).
- `internal/` — daemon (`apid`), host/k0s/sysupdate mechanics (`machine`),
  PKI and machine config (`config`), CLI commands (`cli`).
- `pkg/` — reusable `client` and `version`.

## Building

```sh
just check   # fmt, vet, test, static build
just cross   # linux/amd64 + linux/arm64 static binaries
just proto   # regenerate protobuf/gRPC code (buf)
```

## Onboarding (maintenance mode)

`apxd` generates a node CA, a server identity, and an admin client identity
under its state directory (`-state-dir`, default `/var/lib/archaeopteryx`) on
first start, then serves mTLS gRPC on `:50000`. While unconfigured it is in
**maintenance mode** and prints a one-per-boot onboarding token.

```sh
# workstation: generate the machine config + client bundle (offline)
apxctl gen config factory node1 --endpoint 10.0.0.5:50000 --hostname node1 \
       --output-dir ./apx
# -> ./apx/apxconfig.yaml, apxctl.yaml, ca.crt, admin.crt, admin.key

# node admin shell: apply the config with the printed token
apxctl --endpoint 127.0.0.1:50000 \
       --ca /var/lib/archaeopteryx/ca.crt \
       --cert /var/lib/archaeopteryx/admin.crt \
       --key /var/lib/archaeopteryx/admin.key \
       apply-config ./apx/apxconfig.yaml --maintenance-token <token>

# workstation: connect over the new trust anchor
apxctl --bundle ./apx/apxctl.yaml status
apxctl --bundle ./apx/apxctl.yaml config get
```

Applying the config bakes the hostname, swaps the CA and server identity, and
drops the bootstrap CA key + onboarding token, so the bootstrap admin identity
stops working; use the generated bundle afterwards.

## Systemd / journal

```sh
apxctl --bundle ./apx/apxctl.yaml services              # unit states
apxctl --bundle ./apx/apxctl.yaml service restart k0scontroller.service
apxctl --bundle ./apx/apxctl.yaml logs --tail 200       # last 200 lines
apxctl --bundle ./apx/apxctl.yaml logs -f -u k0scontroller.service
apxctl --bundle ./apx/apxctl.yaml logs --since "1 hour ago"
```

## k0s

```sh
apxctl --bundle ./apx/apxctl.yaml bootstrap            # sysext merge + controller + wait Ready
apxctl --bundle ./apx/apxctl.yaml kubeconfig           # admin kubeconfig on stdout
apxctl --bundle ./apx/apxctl.yaml kubeconfig --merge   # merge as context apx-<host>
apxctl --bundle ./apx/apxctl.yaml kubeconfig --file ./kubeconfig
```

## Update / rollback

```sh
apxctl --bundle ./apx/apxctl.yaml update --check            # plan only (streams the table)
apxctl --bundle ./apx/apxctl.yaml update                    # apply both axes, staged reboot
apxctl --bundle ./apx/apxctl.yaml update --component=k0s    # only the k0s sysext (no reboot)
apxctl --bundle ./apx/apxctl.yaml update --reboot           # apply, then per rebootStrategy
apxctl --bundle ./apx/apxctl.yaml rollback                  # boot the previous UKI next
apxctl --bundle ./apx/apxctl.yaml rollback --reboot         # ...and reboot now
```

`update` auto-determines the flow: OS-only updates are staged and need a reboot
(kured's `/run/reboot-required` hook covers it with the `staged` strategy) while
k0s-only updates are merged into the running sysext tree immediately. `rollback`
pins the other A/B slot via `systemd-boot set-oneshot`.

## Lifecycle

```sh
apxctl reboot                       # graceful reboot
apxctl reboot --mode poweroff      # power off instead
apxctl shutdown                     # power off
apxctl reset                        # stop k0s + wipe cluster state, then reboot
apxctl reset --wipe                 # factory reset: also drop config/TLS -> maintenance mode
apxctl events                       # stream machine/update/k0s events (Ctrl-C to stop)
```

`reset` without `--wipe` keeps the node's apx identity; `--wipe` removes the
machine config and TLS material too, so the node boots back into maintenance
mode. `events` tails the journal, surfacing records from k0s, sysupdate/sysext,
the apx daemon, and the kernel, plus anything at error priority. A `--context
<name>` global flag selects a named bundle context when a bundle has several.

## Packaging + smoke tests

```sh
just image-tree     # stage dist/rootfs: apx binaries + systemd unit + preset
just smoke          # loopback E2E: gen config -> onboarding -> apply-config -> checks
just cross          # static linux/amd64 + linux/arm64 binaries
just qemu-smoke     # full VM smoke; needs MICRORAPTOR_IMAGE from the Microraptor repo
```

`packaging/microraptor/` holds the systemd unit and preset the Microraptor DDI
pulls in: `apxd.service` (hardened, root, write paths limited to the apx state,
sysext/k0s dirs, and `/efi`) and `zz-enable-apxd.preset`. `just image-tree`
assembles `dist/rootfs/` in the target layout; `hack/smoke.sh` runs the full
onboarding flow against a local daemon (including config-rotation rejection of
the old bootstrap identity), and `hack/qemu-smoke.sh` drives the same checks
against a real QEMU guest when a Microraptor image is available.

## Development

```sh
just check          # fmt, vet, lint (golangci-lint), test, build
just test ./internal/machine   # run one package's tests
just smoke          # full onboarding E2E against a local daemon
```

Work is tracked milestone-by-milestone in [PLAN.md](PLAN.md); each milestone
lands with unit tests, the milestone bullet flipped to ✅, README command
sections, and a green `just check`. A dedicated agent skill for coding on this
repo ships in `.opencode/skills/archaeopteryx/SKILL.md`.

## Releases

Releases are **manual only** — nothing auto-triggers:

```sh
# 1. Make sure the tree is green and committed.
just check

# 2. Push the commit(s) you want to release.

# 3. In GitHub → Actions → "Release", click Run workflow, type the tag
#    (e.g. v0.1.0), set prerelease/notes as needed, and run it.
```

The `Release` workflow (`.github/workflows/release.yml`) then:

1. runs the full `just check` gate;
2. cross-compiles static `linux/amd64` + `linux/arm64` binaries with the
   release version baked in (via `APX_BUILD_VERSION`, see `Justfile`);
3. stages per-arch tarballs (`archaeopteryx-<ver>-linux-<arch>.tar.gz` with
   the binaries + systemd unit + preset) and a `SHA256SUMS`;
4. creates the tag and the GitHub Release and attaches the artifacts.

It only runs when you trigger it from the Actions tab (with the tag input) —
there is no push/tag-based automation, so a release never happens without an
explicit confirmation.

## License

Apache-2.0.