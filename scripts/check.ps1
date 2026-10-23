. "$PSScriptRoot/common.ps1"
Import-ProjectEnv
if ([string]::IsNullOrWhiteSpace($env:TEST_DB_DSN)) { throw 'TEST_DB_DSN is required: use a dedicated *_test database so integration tests cannot silently skip.' }
Start-ProjectDatabase
Push-Location $ProjectRoot
try {
    & pnpm check
    Assert-ExitCode 'Type, contract and build checks'
    Push-Location (Join-Path $ProjectRoot 'services/api')
    try {
        & go test ./...
        Assert-ExitCode 'Go tests including MySQL integration'
        & go vet ./...
        Assert-ExitCode 'Go vet'
    } finally { Pop-Location }
    & pnpm test:e2e
    Assert-ExitCode 'Browser integration tests'
} finally { Pop-Location }
