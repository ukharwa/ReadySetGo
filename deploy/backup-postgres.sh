#!/bin/sh
# Writes a compressed dump of the Postgres database into backups/, keeps the
# newest KEEP (default 7), and copies the new dump off the server with rclone
# when BACKUP_REMOTE names an rclone destination (for example
# "b2:my-bucket/myapp"). Run it from cron on the server; see
# deploy/backup.cron.example.
#
# Restore a dump (it replaces what the database holds):
#   docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists' < backups/FILE.dump
set -eu

cd "$(dirname "$0")/.."
keep=${KEEP:-7}
mkdir -p backups
file="backups/kickstart-$(date -u +%Y%m%dT%H%M%SZ).dump"

# pg_dump runs inside the database container with its own credentials.
# Writing to a temporary name first means a failed dump never looks finished.
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "$file.tmp"
mv "$file.tmp" "$file"
echo "backup written: $file"

# Keep the newest $keep dumps. Names sort by time: the timestamp is UTC and
# fixed-width.
ls -1 backups/kickstart-*.dump | sort -r | tail -n +"$((keep + 1))" | while read -r old; do
	rm -- "$old"
	echo "old backup deleted: $old"
done

if [ -n "${BACKUP_REMOTE:-}" ]; then
	rclone copy "$file" "$BACKUP_REMOTE"
	echo "backup copied to $BACKUP_REMOTE"
fi
