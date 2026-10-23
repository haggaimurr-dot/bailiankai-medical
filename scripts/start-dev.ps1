. "$PSScriptRoot/common.ps1"
Import-ProjectEnv
Start-ProjectDatabase
New-Item -ItemType Directory -Force -Path (Join-Path $LocalRoot 'bin'), (Join-Path $LocalRoot 'logs') | Out-Null
$processFile = Join-Path $LocalRoot 'processes.json'
if (Test-Path -LiteralPath $processFile) {
    $previous = Get-Content -LiteralPath $processFile -Raw | ConvertFrom-Json
    foreach ($entry in $previous) {
        $existing = Get-Process -Id $entry.id -ErrorAction SilentlyContinue
        if ($existing -and $existing.StartTime -and $existing.StartTime.ToUniversalTime().Ticks -eq ([datetime]$entry.startedAt).ToUniversalTime().Ticks) {
            throw 'Project processes are already running. Use scripts/stop-dev.ps1 before restarting.'
        }
    }
}
foreach ($port in @(8080, 5173)) {
    if (Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue) {
        throw "Port $port is occupied. Check its owning process before starting this project."
    }
}
Push-Location (Join-Path $ProjectRoot 'services/api')
try {
    & go run ./cmd/migrate
    Assert-ExitCode 'Database migrations'
    & go run ./cmd/seed
    Assert-ExitCode 'Demo seed'
    $env:EMPLOYEE_CREDENTIALS_FILE = Join-Path $LocalRoot 'employee-credentials.txt'
    & go run ./cmd/bootstrap-employee
    Assert-ExitCode 'Employee bootstrap'
    Remove-Item Env:EMPLOYEE_CREDENTIALS_FILE
    & go build -o (Join-Path $LocalRoot 'bin/bailiankai-api.exe') ./cmd/api
    Assert-ExitCode 'API build'
} finally { Pop-Location }
$started = @()
try {
    $api = Start-Process -FilePath (Join-Path $LocalRoot 'bin/bailiankai-api.exe') -WorkingDirectory (Join-Path $ProjectRoot 'services/api') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $LocalRoot 'logs/api.stdout.log') -RedirectStandardError (Join-Path $LocalRoot 'logs/api.stderr.log')
    $started += @{name='api'; id=$api.Id; startedAt=$api.StartTime.ToUniversalTime().ToString('o')}
    Wait-Endpoint 'http://127.0.0.1:8080/readyz' $api
    $vite = Join-Path $ProjectRoot 'apps/admin/node_modules/vite/bin/vite.js'
    $nodePath = (Get-Command node.exe).Source
    $admin = Start-Process -FilePath $nodePath -ArgumentList @("`"$vite`"", '--host', '127.0.0.1', '--port', '5173', '--strictPort') -WorkingDirectory (Join-Path $ProjectRoot 'apps/admin') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $LocalRoot 'logs/admin.stdout.log') -RedirectStandardError (Join-Path $LocalRoot 'logs/admin.stderr.log')
    $started += @{name='admin'; id=$admin.Id; startedAt=$admin.StartTime.ToUniversalTime().ToString('o')}
    Wait-Endpoint 'http://127.0.0.1:5173' $admin
    Write-Host 'Admin: http://127.0.0.1:5173'
    Write-Host 'API readiness: http://127.0.0.1:8080/readyz'
    Write-Host 'Stop: pwsh -File scripts/stop-dev.ps1 (add -Database to stop MySQL too).'
} finally {
    ConvertTo-Json -InputObject @($started) | Set-Content -LiteralPath $processFile -Encoding utf8
}
