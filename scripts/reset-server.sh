#!/usr/bin/env bash
# Reset and restart the admin server with a fresh database.
#
# Usage:
#   ./scripts/reset-server.sh              # reset + start
#   ./scripts/reset-server.sh --reset-only # just wipe data, don't start
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONFIG="${REPO_ROOT}/admin/config.yaml"
SERVER="${REPO_ROOT}/server"

# Kill any running server
pkill -f "server.*config.*admin" 2>/dev/null && echo "Stopped running server" || true
sleep 1

# Remove all data directories (databases, state)
rm -rf "${REPO_ROOT}/data/" "${REPO_ROOT}/admin/data/" "${REPO_ROOT}/cmd/server/data/"
echo "Cleared all data directories"

if [[ "${1:-}" == "--reset-only" ]]; then
    echo "Reset complete (server not started)"
    exit 0
fi

# Build
echo "Building server..."
(cd "$REPO_ROOT" && go build -o server ./cmd/server)

# Start
echo "Starting server with admin config..."
cd "$REPO_ROOT"
"$SERVER" -config "$CONFIG" &
SERVER_PID=$!
echo "Server PID: $SERVER_PID"

# Wait for ready
for i in $(seq 1 10); do
    if curl -sf http://localhost:8081/api/v1/auth/setup-status >/dev/null 2>&1; then
        echo "Server ready at http://localhost:8081"
        curl -s http://localhost:8081/api/v1/auth/setup-status
        exit 0
    fi
    sleep 1
done

echo "WARNING: Server did not respond within 10 seconds"
exit 1
