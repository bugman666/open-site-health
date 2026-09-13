#!/usr/bin/env bash
# Compile-and-run smoke: start osh, wait for /healthz, then exit.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${ROOT}/bin/osh"
if [[ ! -x "${BIN}" ]]; then
  (cd "${ROOT}" && go build -o bin/osh ./cmd/osh)
fi

WORKDIR="$(mktemp -d)"
trap 'if [[ -n "${PID:-}" ]]; then kill "${PID}" 2>/dev/null || true; wait "${PID}" 2>/dev/null || true; fi; rm -rf "${WORKDIR}"' EXIT

PORT="${OSH_SMOKE_PORT:-18080}"
export OSH_LISTEN="127.0.0.1:${PORT}"
export OSH_DATA_DIR="${WORKDIR}/data"
export OSH_CONFIG="${ROOT}/configs/config.example.json"

"${BIN}" >"${WORKDIR}/osh.log" 2>&1 &
PID=$!

for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:${PORT}/healthz" -o "${WORKDIR}/health.json"; then
    if grep -q '"status":"ok"' "${WORKDIR}/health.json"; then
      echo "smoke: /healthz ok"
      cat "${WORKDIR}/health.json"
      exit 0
    fi
    echo "smoke: unexpected body:" >&2
    cat "${WORKDIR}/health.json" >&2
    exit 1
  fi
  if ! kill -0 "${PID}" 2>/dev/null; then
    echo "smoke: process exited early" >&2
    cat "${WORKDIR}/osh.log" >&2
    exit 1
  fi
  sleep 0.1
done

echo "smoke: timed out waiting for /healthz" >&2
cat "${WORKDIR}/osh.log" >&2
exit 1
