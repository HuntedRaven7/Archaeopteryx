# Archaeopteryx

A Talos-like control plane for [Microraptor](https://github.com/HuntedRaven7/Microraptor):
a node-side gRPC API daemon (`apxd`) and a CLI (`apxctl`) for k0s single-node
factory images built on `systemd-sysupdate` A/B slots.

See [PLAN.md](PLAN.md) for the architecture and the implementation roadmap.

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

`rollback`, and lifecycle commands land in the upcoming milestones tracked in
[PLAN.md](PLAN.md).

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

## License

Apache-2.0.