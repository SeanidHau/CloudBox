#!/usr/bin/env sh
set -eu

# Back up the two durable production stores: PostgreSQL metadata and MinIO
# objects. Redis is deliberately excluded because it contains only cache and
# short-lived share access-control state.
COMPOSE_FILE="${COMPOSE_FILE:-compose.production.yaml}"
BACKUP_ROOT="${BACKUP_ROOT:-./backups}"
VOLUME_PREFIX="${COMPOSE_PROJECT_NAME:-cloudbox}"
MINIO_VOLUME="${MINIO_VOLUME:-${VOLUME_PREFIX}_minio-data}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DESTINATION="$BACKUP_ROOT/$STAMP"

mkdir -p "$DESTINATION"

set -a
. ./.env
set +a

docker compose -f "$COMPOSE_FILE" exec -T postgres \
  pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom \
  > "$DESTINATION/postgres.dump"

docker run --rm \
  -v "$MINIO_VOLUME":/source:ro \
  -v "$(cd "$DESTINATION" && pwd)":/backup \
  alpine:3.22 \
  tar -C /source -czf /backup/minio-data.tar.gz .

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$DESTINATION/postgres.dump" "$DESTINATION/minio-data.tar.gz" > "$DESTINATION/SHA256SUMS"
else
  shasum -a 256 "$DESTINATION/postgres.dump" "$DESTINATION/minio-data.tar.gz" > "$DESTINATION/SHA256SUMS"
fi

printf 'Backup written to %s\n' "$DESTINATION"
