"""Initialize an isolated, empty project database. Never access other databases."""
import os
from pathlib import Path
import secrets
import shlex
import subprocess

config = Path('/etc/bailiankai')
root_file = config / 'mysql-root.cnf'
env_file = config / 'api.env'
mysql = '/opt/bailiankai/mysql-8.4.11/bin/mysql'
socket = '/run/bailiankai-mysql/mysql.sock'

if root_file.exists() != env_file.exists():
    raise SystemExit('Partial credentials exist; manual recovery required, no passwords changed')
if root_file.exists():
    subprocess.run([mysql, f'--defaults-extra-file={root_file}', '-e', 'SELECT 1'], check=True)
    print('Existing project credentials retained')
    raise SystemExit(0)

root_password, app_password = secrets.token_hex(32), secrets.token_hex(32)
sql = f"""
CREATE DATABASE bailiankai CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'bailiankai'@'127.0.0.1' IDENTIFIED BY '{app_password}';
GRANT ALL PRIVILEGES ON bailiankai.* TO 'bailiankai'@'127.0.0.1';
ALTER USER 'root'@'localhost' IDENTIFIED BY '{root_password}';
"""
subprocess.run([mysql, '--protocol=socket', f'--socket={socket}', '-uroot'], input=sql.encode(), check=True)
os.umask(0o077)
root_file.write_text(f'[client]\nuser=root\npassword={root_password}\nsocket={socket}\nprotocol=socket\n')
values = {
    'APP_ENV': 'server', 'ENABLE_DEMO': 'false',
    'HTTP_ADDR': '127.0.0.1:18081',
    'DB_DSN': f'bailiankai:{app_password}@tcp(127.0.0.1:13307)/bailiankai?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai',
    'MEDIA_DRIVER': 'local', 'MEDIA_ROOT': '/var/lib/bailiankai/private-media',
    'ADMIN_ALLOWED_ORIGINS': 'https://42.192.114.201',
    'WECHAT_APP_ID': '', 'WECHAT_APP_SECRET': '',
    'EMPLOYEE_CREDENTIALS_FILE': '/var/lib/bailiankai/bootstrap/employee-credentials.txt',
}
env_file.write_text(''.join(f'{key}={shlex.quote(value)}\n' for key, value in values.items()))
root_file.chmod(0o600)
env_file.chmod(0o600)
print('Independent project database and private credentials initialized')
