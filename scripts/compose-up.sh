#!/usr/bin/env bash
# Require OSH_API_TOKEN with a human-readable hint, then docker compose up.
# The container listens on :8080, so a token is mandatory (same rule as
# docker-compose.yml ${OSH_API_TOKEN:?…} and process Validate()).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${ROOT}"

token="${OSH_API_TOKEN-}"
token="${token#"${token%%[![:space:]]*}"}"
token="${token%"${token##*[![:space:]]}"}"

if [[ -z "${token}" ]]; then
  cat >&2 <<'EOF'
OSH_API_TOKEN is not set.

Compose publishes the process on :8080 inside the container (not loopback),
so a shared API token is required. The service will not start without one.

未设置 OSH_API_TOKEN。容器内监听 :8080（不是 127.0.0.1），必须先设置共享 token，否则进程会拒绝启动。

  export OSH_API_TOKEN=$(openssl rand -hex 16)
  ./scripts/compose-up.sh

Or: make compose-up

Do not skip the token. See README 「如何运行」.
EOF
  exit 1
fi

exec docker compose up --build -d "$@"
