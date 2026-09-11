# Microraptor packaging drop-in

Files here assemble into the Microraptor DDI. `just image-tree` stages them
(plus the static binaries) into `dist/rootfs/` in the target layout:

```
usr/bin/apxd
usr/bin/apxctl
usr/lib/systemd/system/apxd.service
usr/lib/systemd/system-preset/zz-enable-apxd.preset
```

Wire the tree into the Microraptor BuildStream project:

- Create `elements/microraptor/os-apx.bst` that copies `dist/rootfs/`
  content in, and add `os-apx` to `os-stack.bst`. (`just image-tree` is the
  source step; the BuildStream element is a plain file-copy.)

  ```yaml
  kind: copy
  description: |

    Archaeopteryx control plane (apxd + apxctl).

    Build the static, versioned binaries and the unit/preset files with
    `just image-tree` from the Archaeopteryx tree, then copy them in:
    @substitutions, or a `files/` dir checked into microraptor, works.

  sources:
    - kind: local
      directory: archaeopteryx/dist/rootfs
      track: master
  depends:
    - elements/freedesktop-sdk.bst  # runtime, nothing extra
  config:
    layout: [
      ("usr/", "usr/"),
    ]
  ```

- Persist the node state: `/var/lib/archaeopteryx` (PKI + `config.yaml`) is
  already covered by the existing `var.mount`. No extra mounts needed.

- apxd runs before network/k0s lifecycle but `After=network-online.target`;
  logs to the journal. First boot the node is unconfigured: apxd prints the
  per-boot onboarding token to the console/journal (maintenance mode), and
  `apxctl apply-config` configures it, after which `zz-enable-apxd.preset`
  keeps it enabled across reboots.

- Update verification keys (`sysupdate.keys` / `SHA256SUMS` signing pubkeys)
  live in the Microraptor image and are reused by `systemd-sysupdate`; apxd
  only orchestrates `features`/`pending`/`update`, so no key handling is
  duplicated here.

Layout notes

- `apxd.service` runs as root: it drives `systemctl` reboots, `systemd-sysext
  merge` (mounts), and `bootctl set-oneshot`. Write access is narrowed to
  `/var/lib/{archaeopteryx,extensions,k0s}` and `/efi` via `ProtectSystem=strict`.
- The service ships a preset so enablement is declarative; the image never
  ships an SSH/admin apx account — SSH belongs to Microraptor.