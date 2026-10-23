$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$apiRoot = Join-Path $projectRoot 'services/api'
$outputRoot = Join-Path $projectRoot '.local/cloudrun'
New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
$archivePath = Join-Path $outputRoot 'bailiankai-api-source.zip'
# Create a source-only archive. Never include workspace .env, uploads or .local.
$sourceFiles = @('Dockerfile', '.dockerignore', 'go.mod', 'go.sum') | ForEach-Object { Get-Item -LiteralPath (Join-Path $apiRoot $_) }
$sourceFiles += Get-ChildItem -LiteralPath (Join-Path $apiRoot 'cmd'), (Join-Path $apiRoot 'internal') -Recurse -File | Where-Object {
    ($_.Extension -in @('.go', '.sql')) -and $_.Name -notlike '*_test.go' -and $_.FullName -notmatch '[\\/]testdata[\\/]'
}
$archiveStream = [System.IO.File]::Open($archivePath, [System.IO.FileMode]::Create, [System.IO.FileAccess]::Write)
$archive = [System.IO.Compression.ZipArchive]::new($archiveStream, [System.IO.Compression.ZipArchiveMode]::Create)
try {
    foreach ($sourceFile in $sourceFiles) {
        $entryName = [System.IO.Path]::GetRelativePath($apiRoot, $sourceFile.FullName).Replace('\', '/')
        [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($archive, $sourceFile.FullName, $entryName) | Out-Null
    }
} finally { $archive.Dispose(); $archiveStream.Dispose() }
Write-Host "Source package: $archivePath"
Get-FileHash -LiteralPath $archivePath -Algorithm SHA256
