#!/bin/bash
set -euo pipefail
exec 9>/var/lock/bailiankai-deploy.lock
flock -n 9 || { echo 'Another project deployment is running'; exit 1; }
release=$(realpath "${1:?absolute release directory required}")
case "$release" in /opt/bailiankai/releases/*) ;; *) echo 'Invalid release path'; exit 1;; esac
test -x "$release/bin/api"
test -f "$release/admin/index.html"
test -x /opt/bailiankai/mysql-8.4.11/bin/mysqld
id bailiankai >/dev/null 2>&1 || useradd --system --home-dir /var/lib/bailiankai --shell /usr/sbin/nologin bailiankai
id bailiankai-db >/dev/null 2>&1 || useradd --system --home-dir /var/lib/bailiankai/mysql --shell /usr/sbin/nologin bailiankai-db
install -d -m 755 /var/lib/bailiankai
install -d -o bailiankai -g bailiankai -m 700 /var/lib/bailiankai/private-media /var/lib/bailiankai/caddy /var/lib/bailiankai/bootstrap
install -d -o bailiankai-db -g bailiankai-db -m 750 /var/lib/bailiankai/mysql /var/log/bailiankai
install -d -m 755 /etc/bailiankai
install -m 644 "$release/deploy/mysql.cnf" /etc/bailiankai/mysql.cnf
install -m 644 "$release/deploy/bailiankai-mysql.service" /etc/systemd/system/bailiankai-mysql.service
if ! test -d /var/lib/bailiankai/mysql/mysql; then
    /opt/bailiankai/mysql-8.4.11/bin/mysqld --defaults-file=/etc/bailiankai/mysql.cnf --initialize-insecure --user=bailiankai-db
fi
systemctl daemon-reload
systemctl enable --now bailiankai-mysql
for attempt in $(seq 1 60); do
    if test -S /run/bailiankai-mysql/mysql.sock; then break; fi
    sleep 1
done
python3 "$release/deploy/initialize-secrets.py"
# Run one-time commands with the private environment, not at API cold starts.
set -a
source /etc/bailiankai/api.env
set +a
runuser -u bailiankai -- "$release/bin/migrate"
/opt/bailiankai/mysql-8.4.11/bin/mysql --defaults-extra-file=/etc/bailiankai/mysql-root.cnf --default-character-set=utf8mb4 bailiankai < "$release/deploy/initialize-clinic.sql"
runuser -u bailiankai -- "$release/bin/bootstrap-employee"
ln -sfn "$release" /opt/bailiankai/current
install -m 644 "$release/deploy/bailiankai-api.service" /etc/systemd/system/bailiankai-api.service
install -m 644 "$release/deploy/bailiankai-web.service" /etc/systemd/system/bailiankai-web.service
if ! test -f /etc/bailiankai/Caddyfile; then
    install -m 644 "$release/deploy/Caddyfile.private" /etc/bailiankai/Caddyfile
fi
/usr/bin/caddy validate --config /etc/bailiankai/Caddyfile --adapter caddyfile
systemctl daemon-reload
systemctl enable bailiankai-api bailiankai-web
systemctl restart bailiankai-api
for attempt in $(seq 1 40); do
    if curl -fsS http://127.0.0.1:18081/readyz; then break; fi
    sleep 1
done
curl -fsS http://127.0.0.1:18081/readyz
systemctl restart bailiankai-web
curl --retry 8 --retry-connrefused --retry-delay 1 -fsS http://127.0.0.1:9080/readyz
echo 'PRIVATE_DEPLOYMENT_READY'
