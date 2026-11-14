param([switch]$Diagnostics)

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

$privatePresets = Join-Path $backendRoot 'private\photo-presets.json'
if ($Diagnostics) {
    if ($env:APP_ENV -and $env:APP_ENV -ne 'development') { throw 'Diagnostics requires APP_ENV=development.' }
    $env:APP_ENV = 'development'
    $env:LOCAL_DIAGNOSTICS = 'true'
    $env:HTTP_ADDRESS = '127.0.0.1:8080'
}
if (-not $env:PHOTO_PRESETS_FILE -and (Test-Path -LiteralPath $privatePresets)) {
    $env:PHOTO_PRESETS_FILE = $privatePresets
}
if ((-not $env:YANDEX_API_KEY -or -not $env:YANDEX_FOLDER_ID) -and -not $env:PHOTO_PRESETS_FILE) {
    throw 'Fill YANDEX_API_KEY and YANDEX_FOLDER_ID in backend/.env or configure PHOTO_PRESETS_FILE.'
}

Push-Location -LiteralPath $backendRoot
try {
    $listenAddress = if ($env:HTTP_ADDRESS) { $env:HTTP_ADDRESS } elseif ($env:PORT) { ":$env:PORT" } else { ':8080' }
    $browserAddress = if ($listenAddress.StartsWith(':')) { "localhost$listenAddress" } elseif ($listenAddress.StartsWith('0.0.0.0:')) { $listenAddress -replace '^0\.0\.0\.0:', 'localhost:' } else { $listenAddress }
    Write-Host "ВАЙБЧЕК запускается: http://$browserAddress/ . Сервер работает до Ctrl+C."
    & go run ./cmd/api
    if ($LASTEXITCODE -ne 0) { throw "go run failed with exit code $LASTEXITCODE" }
} finally {
    Pop-Location
}
