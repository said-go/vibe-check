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
    $probe = New-Object System.Diagnostics.Process
    $probe.StartInfo.FileName = (Get-Command docker).Source
    $probe.StartInfo.Arguments = 'info --format {{.Server.Version}}'
    $probe.StartInfo.UseShellExecute = $false
    $probe.StartInfo.CreateNoWindow = $true
    $probe.StartInfo.RedirectStandardOutput = $true
    $probe.StartInfo.RedirectStandardError = $true
    try {
        if (-not $probe.Start()) { return $false }
        $stdout = $probe.StandardOutput.ReadToEndAsync()
        $stderr = $probe.StandardError.ReadToEndAsync()
        if (-not $probe.WaitForExit(8000)) {
            $probe.Kill()
            Write-Host 'Docker daemon did not answer within 8 seconds.'
            return $false
        }
        return $probe.ExitCode -eq 0
    } catch {
        return $false
    } finally {
        $probe.Dispose()
    }
}

Assert-Tool 'go'
Assert-Tool 'node'

# A child PowerShell keeps the regression tool stubs out of this process.
Run-Checked (Get-Process -Id $PID).Path @('-NoProfile', '-File', (Join-Path $projectRoot 'scripts/test-deployment.ps1'))

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
Run-Checked 'node' @((Join-Path $projectRoot 'scripts/build-pwa.mjs'), '--check')
$frontendTests = @(Get-ChildItem -LiteralPath (Join-Path $projectRoot 'tests') -Filter '*.test.cjs' | ForEach-Object { $_.FullName })
Run-Checked 'node' (@('--test') + $frontendTests)

if ($BuildImage) {
    Assert-Tool 'docker'
    if (-not (Test-DockerDaemon)) {
        throw 'Docker daemon is unavailable; start Docker before building the image.'
    }
    Push-Location -LiteralPath $projectRoot
    try {
        Run-Checked 'docker' @('build', '--platform', 'linux/amd64', '--tag', 'vibe-check:preflight', '.')
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
