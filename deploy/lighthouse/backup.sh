#!/bin/bash
set -euo pipefail
umask 077
exec 9>/var/lock/bailiankai-backup.lock
flock -n 9 || exit 0
install -d -m 700 /var/backups/bailiankai
snapshot=/var/backups/bailiankai/$(date -u +%Y%m%dT%H%M%SZ)
mkdir -m 700 "$snapshot"
/opt/bailiankai/mysql-8.4.11/bin/mysqldump --defaults-extra-file=/etc/bailiankai/mysql-root.cnf --single-transaction --routines --triggers --set-gtid-purged=OFF bailiankai | gzip > "$snapshot/database.sql.gz"
tar -czf "$snapshot/private-media.tar.gz" -C /var/lib/bailiankai private-media
cp -p /etc/bailiankai/api.env "$snapshot/api.env"
sha256sum "$snapshot/database.sql.gz" "$snapshot/private-media.tar.gz" > "$snapshot/SHA256SUMS"
touch "$snapshot/COMPLETE"
echo "Backup completed: $snapshot"
