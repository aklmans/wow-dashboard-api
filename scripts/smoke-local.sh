#!/bin/sh

set -u

MAKE_CMD=${MAKE_CMD:-make}
PORT=${PORT:-7272}
BASE_URL="http://localhost:${PORT}"
HEALTH_URL="${BASE_URL}/healthz"
HEALTH_TIMEOUT_SECONDS=${SMOKE_LOCAL_HEALTH_TIMEOUT_SECONDS:-30}
DB_URL=${SMOKE_LOCAL_DATABASE_URL:-${LOCAL_DATABASE_URL:-postgres://spec:spec@localhost:5432/wow_dashboard_api?sslmode=disable}}
API_LOG=${SMOKE_LOCAL_API_LOG:-tmp/smoke-local-api.log}
API_PID=""
API_LISTENER_PIDS=""

fail() {
  echo "smoke-local: $*" >&2
  exit 1
}

port_in_use() {
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"${PORT}" -sTCP:LISTEN >/dev/null 2>&1
    return $?
  fi

  if command -v nc >/dev/null 2>&1; then
    nc -z 127.0.0.1 "${PORT}" >/dev/null 2>&1
    return $?
  fi

  return 1
}

listener_pids() {
  if command -v lsof >/dev/null 2>&1; then
    lsof -tiTCP:"${PORT}" -sTCP:LISTEN 2>/dev/null || true
  fi
}

cleanup() {
  status=$?
  trap - EXIT INT TERM

  if [ -n "${API_PID}" ]; then
    echo "==> Stopping smoke-local API process"
    if kill -0 "${API_PID}" 2>/dev/null; then
      kill "${API_PID}" 2>/dev/null || true
    fi

    for pid in ${API_LISTENER_PIDS}; do
      if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
        kill "${pid}" 2>/dev/null || true
      fi
    done

    wait "${API_PID}" 2>/dev/null || true
  fi

  exit "${status}"
}

trap cleanup EXIT INT TERM

case "${HEALTH_TIMEOUT_SECONDS}" in
  ''|*[!0-9]*) fail "SMOKE_LOCAL_HEALTH_TIMEOUT_SECONDS must be a positive integer" ;;
esac

if [ "${HEALTH_TIMEOUT_SECONDS}" -le 0 ]; then
  fail "SMOKE_LOCAL_HEALTH_TIMEOUT_SECONDS must be greater than zero"
fi

command -v curl >/dev/null 2>&1 || fail "curl is required to wait for ${HEALTH_URL}"
command -v go >/dev/null 2>&1 || fail "go is required to run ./cmd/api"

if port_in_use; then
  fail "port ${PORT} is already in use. Stop the existing local service, or run 'make postman-test' if you want to test the service that is already running."
fi

mkdir -p "$(dirname "${API_LOG}")"
: >"${API_LOG}"

echo "==> Starting local PostgreSQL"
"${MAKE_CMD}" --silent --no-print-directory compose-up || fail "compose-up failed"

echo "==> Preparing local database"
LOCAL_DATABASE_URL="${DB_URL}" "${MAKE_CMD}" --silent --no-print-directory local-setup || fail "local-setup failed"

echo "==> Starting API at ${BASE_URL}"
echo "==> API logs: ${API_LOG}"
DATABASE_URL="${DB_URL}" PORT="${PORT}" go run ./cmd/api >"${API_LOG}" 2>&1 &
API_PID=$!

elapsed=0
while [ "${elapsed}" -lt "${HEALTH_TIMEOUT_SECONDS}" ]; do
  if ! kill -0 "${API_PID}" 2>/dev/null; then
    fail "API process exited before ${HEALTH_URL} became healthy. See ${API_LOG}."
  fi

  if curl -fsS "${HEALTH_URL}" >/dev/null 2>&1; then
    API_LISTENER_PIDS=$(listener_pids)
    echo "==> API is healthy"
    echo "==> Running Newman smoke tests"
    POSTMAN_BASE_URL="${BASE_URL}" "${MAKE_CMD}" --silent --no-print-directory postman-test
    exit $?
  fi

  sleep 1
  elapsed=$((elapsed + 1))
done

fail "timed out after ${HEALTH_TIMEOUT_SECONDS}s waiting for ${HEALTH_URL}. See ${API_LOG}."
