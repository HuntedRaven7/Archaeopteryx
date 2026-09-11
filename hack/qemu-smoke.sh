#!/usr/bin/env bash
# QEMU smoke test for the apx control plane against a real Microraptor DDI.
#
# Runs the same read-only + update checks as hack/smoke.sh, but against a full
# QEMU VM. Requires a built image; builds of the DDI live in the Microraptor
# repository (elements/microraptor/os-apx.bst stages `just image-tree`).
#
#   MICRORAPTOR_IMAGE=/path/to/microraptor.raw just qemu-smoke
#
# Optional knobs (env):
#   APX_LISTEN      host port forwarded to the guest's apx :50000 (default 51027)
#   APX_SEED_DIR    host dir exported to the guest over 9p (mount_tag=apx-seed)
#   QEMU            qemu-system-<arch> binary override (default: auto from $ARCH)
#   ARCH            amd64|arm64 (default amd64)
#   OVMF            path to the EFI firmware (default: auto-detect OVMF_CODE.fd)
#
# The image is expected to come up with the apx identity baked in (matching the
# ship flow); if it boots into maintenance mode instead, connect on the serial
# console, run apply-config with the onboarding token, then re-run this script.
set -euo pipefail

cd "$(dirname "$0")/.."

IMAGE="${MICRORAPTOR_IMAGE:-}"
[[ -n "${IMAGE}" ]] || {
  cat >&2 <<'EOF'
qemu-smoke: MICRORAPTOR_IMAGE not set.

Set it to a bootable Microraptor image (raw DDI, EFI/SD boot) built from the
Microraptor repository after staging `just image-tree`:

  MICRORAPTOR_IMAGE=/path/to/microraptor-<ver>.raw just qemu-smoke

The image must have the apx identity baked in (os-apx element flow). If you are
iterating on an image that boots into maintenance mode, use the serial console
to onboard it first.
EOF
  exit 1
}

ARCH="${ARCH:-amd64}"
PORT="${APX_LISTEN:-51027}"
QEMU="${QEMU:-qemu-system-${ARCH}}"
command -v "${QEMU}" >/dev/null 2>&1 || { echo "qemu-smoke: ${QEMU} not found" >&2; exit 1; }

WORK="${APX_SMOKE_WORK:-$(mktemp -d /tmp/apx-qemu.XXXXXX)}"
CONSOLE="${WORK}/console.log"
: >"${CONSOLE}"
APXD_TRIES=80

# --accel: prefer hardware acceleration, fall back to TCG.
ACCEL=""
if "${QEMU}" -accel help 2>/dev/null | grep -q kvm; then
  ACCEL="-accel kvm -cpu host"
else
  echo "qemu-smoke: KVM unavailable; using TCG (slow)" >&2
  ACCEL="-accel tcg"
fi

# EFI firmware for amd64/arm64 DDI images (CD boot mostly for real systems).
FIRMWARE=()
if [[ -z "${OVMF:-}" ]]; then
  for cand in \
    /usr/share/OVMF/OVMF_CODE.fd \
    /usr/share/ovmf/OVMF_CODE.fd \
    /usr/share/edk2/x64/OVMF_CODE.fd; do
    [[ -f "${cand}" ]] && { OVMF="${cand}"; break; }
  done
fi
if [[ -n "${OVMF:-}" && -f "${OVMF}" ]]; then
  FIRMWARE=(-drive "if=pflash,format=raw,readonly=on,file=${OVMF}")
else
  echo "qemu-smoke: no EFI firmware found; hoping the image is BIOS-bootable" >&2
fi

SEED=()
if [[ -n "${APX_SEED_DIR:-}" ]]; then
  [[ -d "${APX_SEED_DIR}" ]] || { echo "qemu-smoke: missing --seed dir ${APX_SEED_DIR}" >&2; exit 1; }
  SEED=(-virtfs "local,path=${APX_SEED_DIR},mount_tag=apx-seed,security_mode=none")
fi

GDBUS=(-netdev user,id=net0,hostfwd=tcp:127.0.0.1:${PORT}-:50000 -device virtio-net-pci,netdev=net0)
MACHINE="-m 2G -smp 2"

echo "qemu-smoke: booting ${IMAGE} (serial console → ${CONSOLE})"
begin=$(date +%s)
"${QEMU}" "${ACCEL}" "${MACHINE}" "${GDBUS[@]}" "${SEED[@]}" "${FIRMWARE[@]}" \
  -drive "file=${IMAGE},format=raw,if=virtio" \
  -serial "file:${CONSOLE}" -monitor none -display none -daemonize -pidfile "${WORK}/qemu.pid" \
  || { echo "qemu-smoke: failed to start ${QEMU}" >&2; exit 1; }

cleanup() {
  [[ -f "${WORK}/qemu.pid" ]] && { kill "$(cat "${WORK}/qemu.pid")" 2>/dev/null || true; }
}
trap cleanup EXIT

port_open() { (exec 3<>"/dev/tcp/127.0.0.1/${PORT}") 2>/dev/null; }

echo "qemu-smoke: waiting for apx on 127.0.0.1:${PORT} (max ${APXD_TRIES}x2s)..."
for i in $(seq 1 "${APXD_TRIES}"); do
  if port_open; then echo "  apx reachable after ${i} tries ($(( $(date +%s) - begin ))s)"; break; fi
  if [[ $i -eq ${APXD_TRIES} ]]; then
    echo "qemu-smoke: apx never came up; last console lines:" >&2
    tail -n 20 "${CONSOLE}" >&2 || true
    exit 1
  fi
  sleep 2
done

BUNDLE=(--endpoint "127.0.0.1:${PORT}" --insecure)
[[ -n "${APX_BUNDLE:-}" ]] && BUNDLE=(--bundle "${APX_BUNDLE}")

CHECKS=("version" "status" "services" "logs --tail 5")
for c in "${CHECKS[@]}"; do
  echo "  run: apxctl ${c}"
  ./dist/apxctl "${BUNDLE[@]}" ${c} >/dev/null 2>&1 || { echo "qemu-smoke: ${c} failed" >&2; exit 1; }
done

if ./dist/apxctl "${BUNDLE[@]}" update --check >/dev/null 2>&1; then
  echo "  run: update --check (plan ok)"
else
  echo "  run: update --check (degraded gracefully)"
fi

echo
echo "QEMU SMOKE PASS (bundle endpoint 127.0.0.1:${PORT}, work ${WORK})"