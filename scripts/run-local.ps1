$ErrorActionPreference = 'Stop'
$backendRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\backend')).Path
$envFile = Join-Path $backendRoot '.env'
if (-not (Test-Path -LiteralPath $envFile -PathType Leaf)) {
    throw 'Create backend/.env before starting the local server.'
}

foreach ($line in [System.IO.File]::ReadAllLines($envFile)) {
    if ($line -match '^\s*(?:#|$)') { continue }
    if ($line -notmatch '^([A-Z][A-Z0-9_]*)=(.*)$') { throw 'Invalid line in backend/.env.' }
    [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2], 'Process')
}

if (-not $env:YANDEX_API_KEY -or -not $env:YANDEX_FOLDER_ID) {
    throw 'Fill YANDEX_API_KEY and YANDEX_FOLDER_ID in backend/.env before starting AI analysis.'
}

Push-Location -LiteralPath $backendRoot
try {
    & go run ./cmd/api
    if ($LASTEXITCODE -ne 0) { throw "go run failed with exit code $LASTEXITCODE" }
} finally {
    Pop-Location
}
