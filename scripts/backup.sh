#!/usr/bin/env bash
# Back up the RelayForge PostgreSQL database with pg_dump.
#
# Usage:
#   BACKUP_DIR=/var/backups/relayforge ./scripts/backup.sh
#
# By default the script runs pg_dump inside the running `postgres` Compose
# service. Set DATABASE_URL to dump a remote database directly instead.
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-./backups}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
POSTGRES_DB="${POSTGRES_DB:-relayforge}"
POSTGRES_USER="${POSTGRES_USER:-relayforge}"

mkdir -p "$BACKUP_DIR"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
output="$BACKUP_DIR/relayforge-$timestamp.dump"

if [[ -n "${DATABASE_URL:-}" ]]; then
  pg_dump --format=custom --no-owner --no-privileges --file="$output" "$DATABASE_URL"
else
  docker compose exec -T postgres \
    pg_dump --format=custom --no-owner --no-privileges \
    -U "$POSTGRES_USER" "$POSTGRES_DB" > "$output"
fi

# Refuse to keep an empty or suspiciously small dump.
size="$(stat -c '%s' "$output" 2>/dev/null || stat -f '%z' "$output")"
if [[ "$size" -lt 1024 ]]; then
  echo "backup failed: $output is only $size bytes" >&2
  exit 1
fi

find "$BACKUP_DIR" -type f -name 'relayforge-*.dump' -mtime +"$RETENTION_DAYS" -delete
echo "backup written to $output ($size bytes); retaining $RETENTION_DAYS days"
