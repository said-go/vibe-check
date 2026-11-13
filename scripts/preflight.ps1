param(
    [switch]$BuildImage,
    [switch]$RequireCloudTools
)

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$backendRoot = Join-Path $projectRoot 'backend'

function Assert-Tool([string]$Name) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required tool is missing: $Name"
    }
}

function Run-Checked([string]$Executable, [string[]]$Arguments) {
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Executable failed with exit code $LASTEXITCODE"
    }
}

function Test-DockerDaemon {
    $previousPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & docker info --format '{{.Server.Version}}' 2>$null | Out-Null
        return $LASTEXITCODE -eq 0
    } finally {
        $ErrorActionPreference = $previousPreference
    }
}

Assert-Tool 'go'
Assert-Tool 'node'

Push-Location -LiteralPath $backendRoot
try {
    Run-Checked 'go' @('test', './...')
    Run-Checked 'go' @('vet', './...')
} finally {
    Pop-Location
}

Run-Checked 'node' @('--check', (Join-Path $projectRoot 'dist/app.js'))
Run-Checked 'node' @('--check', (Join-Path $projectRoot 'dist/history.js'))
Run-Checked 'node' @('--check', (Join-Path $projectRoot 'dist/share-card.js'))
Run-Checked 'node' @('--check', (Join-Path $projectRoot 'dist/sw.js'))

if ($BuildImage) {
    Assert-Tool 'docker'
    if (-not (Test-DockerDaemon)) {
        throw 'Docker daemon is unavailable; start Docker before building the image.'
    }
    Push-Location -LiteralPath $projectRoot
    try {
        Run-Checked 'docker' @('build', '--tag', 'vibe-check:preflight', '.')
    } finally {
        Pop-Location
    }
}

$dockerInstalled = [bool](Get-Command docker -ErrorAction SilentlyContinue)
$ycInstalled = [bool](Get-Command yc -ErrorAction SilentlyContinue)
$dockerDaemonReady = $false
if ($dockerInstalled) {
    $dockerDaemonReady = Test-DockerDaemon
}
Write-Host "Local checks passed. Docker installed: $dockerInstalled; Docker daemon ready: $dockerDaemonReady; Yandex Cloud CLI installed: $ycInstalled."
Write-Host 'No cloud resources were created, and no secret values were read.'

if ($RequireCloudTools -and (-not $dockerDaemonReady -or -not $ycInstalled)) {
    throw 'Deployment tools are missing. Install and configure Docker and the Yandex Cloud CLI before cloud deployment.'
}
