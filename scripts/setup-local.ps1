# Installs a project-local database, without Windows services or machine-wide changes.
. "$PSScriptRoot/common.ps1"
New-Item -ItemType Directory -Force -Path $LocalRoot, (Join-Path $LocalRoot 'downloads'), (Join-Path $LocalRoot 'mysql'), (Join-Path $LocalRoot 'logs') | Out-Null
if ($MySQLRuntimeRoot -match '[^\x00-\x7F]') { throw 'MySQL needs an ASCII runtime alias path; configure an ASCII LOCALAPPDATA for this process.' }
if (Test-Path -LiteralPath $MySQLRuntimeRoot) {
    $alias = Get-Item -LiteralPath $MySQLRuntimeRoot
    if ($alias.LinkType -ne 'Junction' -or $alias.Target -ne $LocalRoot) { throw "Runtime alias exists and belongs to another location: $MySQLRuntimeRoot" }
} else {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $MySQLRuntimeRoot) | Out-Null
    New-Item -ItemType Junction -Path $MySQLRuntimeRoot -Target $LocalRoot | Out-Null
}
$mysqlZip = Join-Path $LocalRoot 'downloads/mysql-8.4.11-winx64.zip'
$mysqlBin = Get-MySQLBin
if (-not (Test-Path -LiteralPath (Join-Path $mysqlBin 'mysqld.exe'))) {
    if (-not (Test-Path -LiteralPath $mysqlZip)) {
        Write-Host 'Downloading MySQL 8.4.11 (268 MB) into .local...'
        & curl.exe -L --fail --connect-timeout 20 --max-time 900 --retry 2 --silent --show-error 'https://cdn.mysql.com/Downloads/MySQL-8.4/mysql-8.4.11-winx64.zip' -o $mysqlZip
        Assert-ExitCode 'MySQL download'
    }
    $archiveHash = (Get-FileHash -LiteralPath $mysqlZip -Algorithm MD5).Hash.ToLowerInvariant()
    if ($archiveHash -ne '2e833921898a9a030ea6bfe81bd811bc') { throw 'MySQL download checksum mismatch. Remove only the incomplete .local/downloads/mysql-8.4.11-winx64.zip and retry.' }
    Write-Host 'Extracting MySQL...'
    Expand-Archive -LiteralPath $mysqlZip -DestinationPath $LocalRoot -Force
}
$secretsFile = Join-Path $LocalRoot 'dev-secrets.json'
if (-not (Test-Path -LiteralPath $secretsFile)) {
    $secrets = @{
        rootPassword = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(24)).ToLowerInvariant()
        appPassword = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(24)).ToLowerInvariant()
    }
    $secrets | ConvertTo-Json | Set-Content -LiteralPath $secretsFile -Encoding utf8
}
$secrets = Get-Content -LiteralPath $secretsFile -Raw | ConvertFrom-Json
$dataDir = Join-Path $MySQLRuntimeRoot 'mysql/data'
$baseDir = Split-Path -Parent $mysqlBin
$config = Join-Path $MySQLRuntimeRoot 'mysql/my.ini'
$unixBase = $baseDir.Replace('\', '/')
$unixData = $dataDir.Replace('\', '/')
$unixLog = (Join-Path $MySQLRuntimeRoot 'mysql/error.log').Replace('\', '/')
@"
[mysqld]
basedir="$unixBase"
datadir="$unixData"
port=3307
bind-address=127.0.0.1
mysqlx=0
character-set-server=utf8mb4
collation-server=utf8mb4_0900_ai_ci
default-time-zone=+08:00
log-error="$unixLog"
"@ | Set-Content -LiteralPath $config -Encoding utf8NoBOM
@"
[client]
host=127.0.0.1
port=3307
user=root
password=$($secrets.rootPassword)
protocol=tcp
"@ | Set-Content -LiteralPath (Join-Path $LocalRoot 'mysql/client.cnf') -Encoding utf8NoBOM
if (-not (Test-Path -LiteralPath (Join-Path $dataDir 'mysql'))) {
    Write-Host 'Initializing an isolated development database...'
    & (Join-Path $mysqlBin 'mysqld.exe') "--defaults-file=$config" --initialize-insecure
    Assert-ExitCode 'MySQL initialization'
    @"
ALTER USER 'root'@'localhost' IDENTIFIED BY '$($secrets.rootPassword)';
CREATE DATABASE IF NOT EXISTS bailiankai_dev CHARACTER SET utf8mb4;
CREATE DATABASE IF NOT EXISTS bailiankai_test CHARACTER SET utf8mb4;
CREATE USER IF NOT EXISTS 'bailiankai'@'127.0.0.1' IDENTIFIED BY '$($secrets.appPassword)';
GRANT ALL PRIVILEGES ON bailiankai_dev.* TO 'bailiankai'@'127.0.0.1';
GRANT ALL PRIVILEGES ON bailiankai_test.* TO 'bailiankai'@'127.0.0.1';
"@ | Set-Content -LiteralPath (Join-Path $LocalRoot 'mysql/bootstrap.sql') -Encoding utf8NoBOM
}
Start-ProjectDatabase
$envPath = Join-Path $ProjectRoot '.env'
if (-not (Test-Path -LiteralPath $envPath)) {
    @"
APP_ENV=development
ENABLE_DEMO=true
HTTP_ADDR=127.0.0.1:8080
DB_DSN=bailiankai:$($secrets.appPassword)@tcp(127.0.0.1:3307)/bailiankai_dev?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai
TEST_DB_DSN=bailiankai:$($secrets.appPassword)@tcp(127.0.0.1:3307)/bailiankai_test?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai
PENDING_CONFIRMATION_MINUTES=120
"@ | Set-Content -LiteralPath $envPath -Encoding utf8NoBOM
}
Import-ProjectEnv
$adminEnv = Join-Path $ProjectRoot 'apps/admin/.env.local'
if (Test-Path -LiteralPath $adminEnv) {
    $adminEnvContent = Get-Content -LiteralPath $adminEnv
    $adminEnvContent | Where-Object { $_ -notmatch '^VITE_DEMO_ADMIN_TOKEN=' } | Set-Content -LiteralPath $adminEnv -Encoding utf8NoBOM
}
Write-Host 'Local MySQL is ready on 127.0.0.1:3307. Credentials are in ignored local files.'
