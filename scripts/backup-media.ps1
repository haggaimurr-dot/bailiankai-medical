param([Parameter(Mandatory = $true)][string]$EnvironmentFile)
$ErrorActionPreference = 'Stop'
$projectDirectory = Split-Path -Parent $PSScriptRoot
$resolvedEnvironmentFile = (Resolve-Path -LiteralPath $EnvironmentFile).Path
foreach ($line in Get-Content -LiteralPath $resolvedEnvironmentFile -Encoding UTF8) {
    if ($line -match '^([A-Z_][A-Z0-9_]*)=(.*)$') {
        [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2], 'Process')
    }
}
Push-Location (Join-Path $projectDirectory 'services/api')
try {
    & go run ./cmd/media -operation backup
    if ($LASTEXITCODE -ne 0) { throw 'Encrypted private media backup failed. No completed snapshot should be assumed.' }
} finally { Pop-Location }
