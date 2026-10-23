param([switch]$Database)
. "$PSScriptRoot/common.ps1"
$processFile = Join-Path $LocalRoot 'processes.json'
if (Test-Path -LiteralPath $processFile) {
    $entries = Get-Content -LiteralPath $processFile -Raw | ConvertFrom-Json
    foreach ($entry in $entries) {
        $proc = Get-Process -Id $entry.id -ErrorAction SilentlyContinue
        if ($proc -and $proc.StartTime -and $proc.StartTime.ToUniversalTime().Ticks -eq ([datetime]$entry.startedAt).ToUniversalTime().Ticks) {
            Stop-Process -Id $proc.Id
            Write-Host "Stopped $($entry.name)."
        }
    }
    Remove-Item -LiteralPath $processFile
}
if ($Database) {
    if (-not (Test-ProjectDatabase)) { throw 'Project database is not running or its credentials do not match.' }
    & (Join-Path (Get-MySQLBin) 'mysqladmin.exe') "--defaults-extra-file=$(Join-Path $MySQLRuntimeRoot 'mysql/client.cnf')" shutdown
    Assert-ExitCode 'MySQL shutdown'
    Write-Host 'Stopped the project database. Data remains in .local/mysql/data.'
}
