#!/usr/bin/env sh
set -eu

# Restore requires the target stack to be stopped. It replaces the PostgreSQL
# database and MinIO volume, so it must only be run against the intended host.
if [ "$#" -ne 1 ]; then
  echo "usage: $0 <backup-directory>" >&2
  exit 64
fi

BACKUP_DIRECTORY="$1"
COMPOSE_FILE="${COMPOSE_FILE:-compose.production.yaml}"
VOLUME_PREFIX="${COMPOSE_PROJECT_NAME:-cloudbox}"
MINIO_VOLUME="${MINIO_VOLUME:-${VOLUME_PREFIX}_minio-data}"

test -f "$BACKUP_DIRECTORY/postgres.dump"
test -f "$BACKUP_DIRECTORY/minio-data.tar.gz"

if [ -f "$BACKUP_DIRECTORY/SHA256SUMS" ]; then
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$BACKUP_DIRECTORY" && sha256sum -c SHA256SUMS)
  else
    (cd "$BACKUP_DIRECTORY" && shasum -a 256 -c SHA256SUMS)
  fi
fi

set -a
. ./.env
set +a

docker compose -f "$COMPOSE_FILE" down
docker compose -f "$COMPOSE_FILE" up -d postgres
until docker compose -f "$COMPOSE_FILE" exec -T postgres pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" >/dev/null 2>&1; do
  sleep 2
done

docker compose -f "$COMPOSE_FILE" exec -T postgres \
  pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists --no-owner \
  < "$BACKUP_DIRECTORY/postgres.dump"

docker compose -f "$COMPOSE_FILE" down
docker run --rm \
  -v "$MINIO_VOLUME":/target \
  -v "$(cd "$BACKUP_DIRECTORY" && pwd)":/backup:ro \
  alpine:3.22 \
  sh -c 'rm -rf /target/* /target/.[!.]* /target/..?* && tar -C /target -xzf /backup/minio-data.tar.gz'

docker compose -f "$COMPOSE_FILE" up -d
