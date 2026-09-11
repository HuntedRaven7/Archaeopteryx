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

## First boot (current milestone)

`apxd` generates a node CA, a server identity, and an admin client identity
under its state directory (`-state-dir`, default `/var/lib/archaeopteryx`)
on first start, then serves mTLS gRPC on `:50000`.

```sh
# node
apxd -listen 0.0.0.0:50000 -state-dir /var/lib/archaeopteryx

# workstation
apxctl --endpoint <node>:50000 \
       --ca ca.crt --cert admin.crt --key admin.key \
       version
apxctl --endpoint <node>:50000 \
       --ca ca.crt --cert admin.crt --key admin.key \
       status
```

Onboarding (config-based cert provisioning and maintenance mode), `bootstrap`,
`update`/`rollback`, logs/services, and lifecycle commands land in the upcoming
milestones tracked in [PLAN.md](PLAN.md).

## License

Apache-2.0.