$ErrorActionPreference = 'Stop'
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$LocalRoot = Join-Path $ProjectRoot '.local'
# MySQL on Windows cannot reliably resolve Unicode paths in option files.
# This junction only gives the project-owned .local directory an ASCII alias.
$rootHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($ProjectRoot))).Substring(0, 12).ToLowerInvariant()
$MySQLRuntimeRoot = Join-Path $env:LOCALAPPDATA "bailiankai-runtime/$rootHash"
function Import-ProjectEnv {
    $envFile = Join-Path $ProjectRoot '.env'
    if (-not (Test-Path -LiteralPath $envFile)) { throw 'Run scripts/setup-local.ps1 first.' }
    foreach ($line in Get-Content -LiteralPath $envFile -Encoding UTF8) {
        if ($line -match '^([A-Z_][A-Z0-9_]*)=(.*)$') {
            [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2], 'Process')
        }
    }
}
function Assert-ExitCode([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed (exit $LASTEXITCODE)." }
}
function Wait-Endpoint([string]$Url, [System.Diagnostics.Process]$Process, [int]$Attempts = 40) {
    for ($attempt = 0; $attempt -lt $Attempts; $attempt++) {
        $Process.Refresh()
        if ($Process.HasExited) { throw "Service process exited before becoming ready: $Url. Check .local/logs." }
        try {
            $result = Invoke-WebRequest -Uri $Url -TimeoutSec 2 -UseBasicParsing
            if ($result.StatusCode -eq 200) {
                $port = ([uri]$Url).Port
                $listeners = @(Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue)
                if ($listeners.OwningProcess -contains $Process.Id) { return }
            }
        } catch { }
        Start-Sleep -Milliseconds 500
    }
    throw "Service did not become ready: $Url. Check .local/logs."
}
function Get-MySQLBin {
    Join-Path $MySQLRuntimeRoot 'mysql-8.4.11-winx64/bin'
}
function Test-ProjectDatabase {
    $clientConfig = Join-Path $MySQLRuntimeRoot 'mysql/client.cnf'
    $actualData = & (Join-Path (Get-MySQLBin) 'mysql.exe') "--defaults-extra-file=$clientConfig" --batch --skip-column-names --execute='SELECT @@datadir' 2>$null
    if ($LASTEXITCODE -ne 0) { return $false }
    $expected = [IO.Path]::GetFullPath((Join-Path $MySQLRuntimeRoot 'mysql/data')).TrimEnd('\')
    if ([IO.Path]::GetFullPath(($actualData | Select-Object -First 1).Trim()).TrimEnd('\') -ne $expected) {
        throw 'The MySQL listener belongs to a different data directory; refusing to manage it.'
    }
    return $true
}
function Start-ProjectDatabase {
    $bin = Get-MySQLBin
    $clientConfig = Join-Path $MySQLRuntimeRoot 'mysql/client.cnf'
    if (Test-ProjectDatabase) { return }
    if (Get-NetTCPConnection -LocalPort 3307 -State Listen -ErrorAction SilentlyContinue) {
        throw 'Port 3307 is occupied or project database credentials do not match. No existing service was stopped.'
    }
    $config = Join-Path $MySQLRuntimeRoot 'mysql/my.ini'
    $arguments = @("--defaults-file=`"$config`"")
    $bootstrapSQL = Join-Path $MySQLRuntimeRoot 'mysql/bootstrap.sql'
    if (Test-Path -LiteralPath $bootstrapSQL) { $arguments += "--init-file=`"$bootstrapSQL`"" }
    $proc = Start-Process -FilePath (Join-Path $bin 'mysqld.exe') -ArgumentList $arguments -WindowStyle Hidden -PassThru
    $proc.Id | Set-Content -LiteralPath (Join-Path $LocalRoot 'mysql/process.pid')
    for ($attempt = 0; $attempt -lt 80; $attempt++) {
        if (Test-ProjectDatabase) {
            if (Test-Path -LiteralPath $bootstrapSQL) { Remove-Item -LiteralPath $bootstrapSQL }
            return
        }
        if ($proc.HasExited) { throw 'MySQL exited; check .local/mysql/error.log.' }
        Start-Sleep -Milliseconds 500
        $proc.Refresh()
    }
    throw 'MySQL did not become ready; check .local/mysql/error.log.'
}
