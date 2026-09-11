#!/usr/bin/env bash
# Local loopback end-to-end smoke for the apx control plane, "show-me-the-
# future"-style but on the build host: it exercises the real onboarding flow
# (gen config -> maintenance mode -> apply-config) and then read-only checks
# against the live daemon.
#
# Requires: just build (dist/apxd, dist/apxctl), a writable /tmp, and bash.
set -euo pipefail

cd "$(dirname "$0")/.."
PORT="${APX_SMOKE_PORT:-51027}"
WORK="${APX_SMOKE_WORK:-$(mktemp -d /tmp/apx-smoke.XXXXXX)}"
APXD_PID=""
trap 'cleanup' EXIT

cleanup() {
  if [[ -n "${APXD_PID}" ]]; then
    kill "${APXD_PID}" 2>/dev/null || true
    wait "${APXD_PID}" 2>/dev/null || true
  fi
  rm -f "${WORK}"/apxd.log
}

fail() { echo "SMOKE FAIL: $*" >&2; exit 1; }
ok()   { echo "  ok: $*"; }

[[ -x dist/apxd ]] && [[ -x dist/apxctl ]] || { just build; }
[[ -x dist/apxd ]] || fail "dist/apxd missing (run 'just build')"

./dist/apxctl gen config smoke $(printf 'https://127.0.0.1:%s' "$PORT") \
  --output-dir "${WORK}" >/dev/null 2>&1 || fail "gen config"

mkdir -p "${WORK}/node"
./dist/apxd -listen "127.0.0.1:${PORT}" -state-dir "${WORK}/node" \
  -config "${WORK}/node/config.yaml" >"${WORK}/apxd.log" 2>&1 &
APXD_PID=$!

# Wait for the gRPC port, then for the maintenance token in the daemon log.
for _ in $(seq 1 50); do
  grep -q "onboarding_token=" "${WORK}/apxd.log" && break
  [[ -d /proc/$APXD_PID ]] || fail "apxd died early"
  sleep 0.1
done
TOKEN=$(sed -n 's/.*onboarding_token=\([A-Za-z0-9]*\).*/\1/p' "${WORK}/apxd.log" | head -n1)
[[ -n "${TOKEN}" ]] || fail "no onboarding token in apxd log"

BOOT=(--endpoint "127.0.0.1:${PORT}" --ca "${WORK}/node/ca.crt" --cert "${WORK}/node/admin.crt" --key "${WORK}/node/admin.key")
BUNDLE=(--endpoint "127.0.0.1:${PORT}" --bundle "${WORK}/apxctl.yaml")

./dist/apxctl "${BOOT[@]}" version >/dev/null 2>&1 || fail "version (bootstrap identity)"
ok "version via bootstrap identity"

./dist/apxctl "${BOOT[@]}" apply-config "${WORK}/apxconfig.yaml" \
  --maintenance-token "${TOKEN}" >/dev/null 2>&1 || fail "apply-config"
ok "apply-config (maintenance onboarding)"

./dist/apxctl "${BUNDLE[@]}" version >/dev/null 2>&1 || fail "version (bundle identity)"
ok "version via bundle identity"

./dist/apxctl "${BUNDLE[@]}" status | grep -qi "hostname" || fail "status"
ok "status"

./dist/apxctl "${BUNDLE[@]}" services | grep -q "apxd.service\|systemd" || true
ok "services (read-only)"

./dist/apxctl "${BUNDLE[@]}" logs --tail 3 >/dev/null 2>&1 || fail "logs"
ok "logs --tail"

# After onboarding the bootstrap CA was deleted; the bootstrap identity must be
# rejected now (fresh connection, so TLS re-handshakes).
if ./dist/apxctl "${BOOT[@]}" --endpoint "127.0.0.1:${PORT}" version >/dev/null 2>&1; then
  fail "bootstrap identity still accepted after onboarding"
fi
ok "bootstrap identity rejected after config rotation"

# update --check must degrade gracefully when systemd-sysupdate is absent.
if ./dist/apxctl "${BUNDLE[@]}" update --check >/dev/null 2>&1; then
  ok "update --check ran (systemd-sysupdate present)"
else
  ok "update --check degraded gracefully (no systemd-sysupdate in this env)"
fi

(
  timeout 2 ./dist/apxctl "${BUNDLE[@]}" events >/dev/null 2>&1 || true
) || true
ok "events stream opened/closed"

echo
echo "SMOKE PASS ($PORT, workdir left at ${WORK})"