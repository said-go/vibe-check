param(
    [Parameter(Mandatory)][string]$FolderId,
    [Parameter(Mandatory)][string]$RegistryId,
    [Parameter(Mandatory)][string]$ContainerId,
    [Parameter(Mandatory)][string]$ServiceAccountId,
    [Parameter(Mandatory)][string]$SecretId,
    [Parameter(Mandatory)][string]$SecretVersionId,
    [switch]$Apply
)

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path

foreach ($item in @(
    @{ Name = 'FolderId'; Value = $FolderId },
    @{ Name = 'RegistryId'; Value = $RegistryId },
    @{ Name = 'ContainerId'; Value = $ContainerId },
    @{ Name = 'ServiceAccountId'; Value = $ServiceAccountId },
    @{ Name = 'SecretId'; Value = $SecretId },
    @{ Name = 'SecretVersionId'; Value = $SecretVersionId }
)) {
    if ($item.Value -notmatch '^[a-zA-Z0-9_-]{6,80}$') {
        throw "$($item.Name) must be a Yandex Cloud resource ID."
    }
}

if (-not (Get-Command git -ErrorAction SilentlyContinue)) { throw 'Git is required.' }
$commit = (& git -C $projectRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $commit -notmatch '^[0-9a-f]{40}$') { throw 'Cannot read the Git commit.' }
$image = "cr.yandex/$RegistryId/vibe-check:$commit"
$environment = "APP_ENV=production,AI_PROVIDER=yandex,YANDEX_FOLDER_ID=$FolderId,YANDEX_MODEL=qwen3.6-35b-a3b,FRONTEND_DIR=/app/dist,ANALYSES_PER_HOUR=30,ANALYSES_PER_CLIENT_MINUTE=3,MAX_CONCURRENT_ANALYSES=2"
$secret = "environment-variable=YANDEX_API_KEY,id=$SecretId,version-id=$SecretVersionId,key=YANDEX_API_KEY"

Write-Host "Commit: $commit"
Write-Host "Image: $image"
Write-Host "Target container: $ContainerId in folder $FolderId"
Write-Host 'Revision: 1 core, 1GB RAM, 150s timeout, 2 concurrent requests, 1 instance per zone.'
Write-Host 'Secret value is read by the container from Lockbox; this script only receives resource IDs.'

if (-not $Apply) {
    Write-Host 'Plan only. Pass -Apply after cloud access, Docker authentication, and billing limits are configured.'
    return
}

foreach ($tool in @('docker', 'yc')) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is required for -Apply." }
}
if ((& git -C $projectRoot status --porcelain).Length -gt 0) { throw 'Working tree must be clean for -Apply.' }

& docker info --format '{{.Server.Version}}' 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Docker daemon is unavailable.' }

& yc serverless container get --id $ContainerId --folder-id $FolderId --format json | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Target container is unavailable.' }

Push-Location -LiteralPath $projectRoot
try {
    & docker build --platform linux/amd64 -t $image .
    if ($LASTEXITCODE -ne 0) { throw 'Docker build failed.' }
    & docker push $image
    if ($LASTEXITCODE -ne 0) { throw 'Docker push failed.' }
} finally {
    Pop-Location
}

& yc serverless container revision deploy --container-id $ContainerId --folder-id $FolderId --image $image --cores 1 --memory 1GB --execution-timeout 150s --concurrency 2 --zone-instances-limit 1 --service-account-id $ServiceAccountId --environment $environment --secret $secret
if ($LASTEXITCODE -ne 0) { throw 'Revision deploy failed.' }
Write-Host 'Revision deployed. Public access was not changed; run smoke-yandex.ps1 against the HTTPS URL after configuring access.'
