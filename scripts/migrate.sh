#!/usr/bin/env bash
#
# One-click database migration script for Podsync.
#
# Usage:
#   ./scripts/migrate.sh                                    # Use config.toml in current dir
#   ./scripts/migrate.sh --config /path/to/config.toml      # Use a specific config file
#   ./scripts/migrate.sh --type sqlite --dsn /data/db.sqlite  # Migrate SQLite directly
#   ./scripts/migrate.sh --type mysql --dsn "user:pass@tcp(127.0.0.1:3306)/podsync"  # Migrate MySQL directly
#
# The script builds the migrate binary (if needed) and runs the schema migration
# against the configured SQLite or MySQL database.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="$PROJECT_ROOT/bin/migrate"

# Build the migrate binary if it is missing or stale
if [ ! -x "$BINARY" ] || [ "$PROJECT_ROOT/cmd/migrate/main.go" -nt "$BINARY" ]; then
  echo "==> Building migrate binary"
  (cd "$PROJECT_ROOT" && go build -trimpath -o "$BINARY" ./cmd/migrate)
fi

# Pass all arguments through to the migrate binary.
# Defaults to reading config.toml if no arguments are provided.
if [ "$#" -eq 0 ]; then
  exec "$BINARY" --config "$PROJECT_ROOT/config.toml"
else
  exec "$BINARY" "$@"
fi
