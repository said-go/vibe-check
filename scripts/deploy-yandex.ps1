param(
    [Parameter(Mandatory)][string]$FolderId,
    [Parameter(Mandatory)][string]$RegistryId,
    [Parameter(Mandatory)][string]$ContainerId,
    [Parameter(Mandatory)][string]$ServiceAccountId,
    [Parameter(Mandatory)][string]$SecretId,
    [Parameter(Mandatory)][string]$SecretVersionId,
    [string]$RedisSecretId,
    [string]$RedisSecretVersionId,
    [string]$RedisBudgetKey = 'vibe-check:provider-budget',
    [int]$SharedAnalysesPerHour = 30,
    [string]$ImageDigest,
    [switch]$Apply
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'run-command.ps1')
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$reuseImage = $PSBoundParameters.ContainsKey('ImageDigest')
if ($reuseImage -and $ImageDigest -cnotmatch '^sha256:[0-9a-f]{64}$') { throw 'ImageDigest must be an immutable sha256 digest, not a tag or image URL.' }

$redisEnabled = -not [string]::IsNullOrEmpty($RedisSecretId)
if ($redisEnabled -ne (-not [string]::IsNullOrEmpty($RedisSecretVersionId))) { throw 'RedisSecretId and RedisSecretVersionId must be supplied together.' }
if (-not $redisEnabled -and ($PSBoundParameters.ContainsKey('RedisBudgetKey') -or $PSBoundParameters.ContainsKey('SharedAnalysesPerHour'))) { throw 'Redis budget options require Redis secret IDs.' }
if ($redisEnabled) {
    if ($RedisSecretId -notmatch '^[a-zA-Z0-9_-]{6,80}$' -or $RedisSecretVersionId -notmatch '^[a-zA-Z0-9_-]{6,80}$') { throw 'Redis secret IDs must be Yandex Cloud resource IDs.' }
    if ($RedisBudgetKey -notmatch '^[a-zA-Z0-9:_-]{1,128}$' -or $SharedAnalysesPerHour -lt 1 -or $SharedAnalysesPerHour -gt 1000000) { throw 'Invalid Redis budget key or limit.' }
}

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

if (-not $reuseImage) {
if (-not (Get-Command git -ErrorAction SilentlyContinue)) { throw 'Git is required.' }
$commit = (& git -C $projectRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $commit -notmatch '^[0-9a-f]{40}$') { throw 'Cannot read the Git commit.' }
$image = "cr.yandex/$RegistryId/vibe-check:$commit"
} else {
    $image = "cr.yandex/$RegistryId/vibe-check@$ImageDigest"
}
$environment = "APP_ENV=production,AI_PROVIDER=yandex,YANDEX_FOLDER_ID=$FolderId,YANDEX_MODEL=qwen3.6-35b-a3b,FRONTEND_DIR=/app/dist,ANALYSES_PER_HOUR=30,ANALYSES_PER_CLIENT_MINUTE=3,MAX_CONCURRENT_ANALYSES=2"
$secret = "environment-variable=YANDEX_API_KEY,id=$SecretId,version-id=$SecretVersionId,key=YANDEX_API_KEY"
if ($redisEnabled) { $environment += ",REDIS_BUDGET_KEY=$RedisBudgetKey,SHARED_ANALYSES_PER_HOUR=$SharedAnalysesPerHour" }

if (-not $reuseImage) { Write-Host "Commit: $commit" } else { Write-Host 'Existing immutable image: no build or push will run.' }
Write-Host "Image: $image"
Write-Host "Target container: $ContainerId in folder $FolderId"
Write-Host 'Revision: 1 core, 1GB RAM, 180s timeout, 2 concurrent requests, 1 instance per zone.'
Write-Host 'Secret value is read by the container from Lockbox; this script only receives resource IDs.'
if ($redisEnabled) { Write-Host "Shared Redis budget: $SharedAnalysesPerHour attempts per hour; URL supplied by Lockbox." }

if (-not $Apply) {
    Write-Host 'Plan only. Pass -Apply after cloud access, Docker authentication, and billing limits are configured.'
    return
}

$requiredTools = if ($reuseImage) { @('yc') } else { @('docker', 'yc') }
foreach ($tool in $requiredTools) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is required for -Apply." }
}
if (-not $reuseImage) {
$gitStatus = & git -C $projectRoot status --porcelain
if ($LASTEXITCODE -ne 0) { throw 'Cannot check working tree.' }
if ($gitStatus.Length -gt 0) { throw 'Working tree must be clean for -Apply.' }

Invoke-BoundedCommand docker @('info', '--format', '{{.Server.Version}}') 15 | Out-Null
}
Invoke-BoundedCommand yc @('serverless', 'container', 'get', '--id', $ContainerId, '--folder-id', $FolderId, '--format', 'json') 60 | Out-Null
if (-not $reuseImage) {
Invoke-BoundedCommand docker @('build', '--platform', 'linux/amd64', '-t', $image, '.') 900 $projectRoot | Out-Null
Invoke-BoundedCommand docker @('push', $image) 600 | Out-Null
}
$revisionArguments = @('serverless', 'container', 'revision', 'deploy', '--container-id', $ContainerId, '--folder-id', $FolderId, '--image', $image, '--cores', '1', '--memory', '1GB', '--execution-timeout', '180s', '--concurrency', '2', '--zone-instances-limit', '1', '--service-account-id', $ServiceAccountId, '--environment', $environment, '--secret', $secret)
if ($redisEnabled) { $revisionArguments += @('--secret', "environment-variable=REDIS_BUDGET_URL,id=$RedisSecretId,version-id=$RedisSecretVersionId,key=REDIS_BUDGET_URL") }
Invoke-BoundedCommand yc $revisionArguments 300 | Out-Null
Write-Host 'Revision deployed. Public access was not changed; run smoke-yandex.ps1 against the HTTPS URL after configuring access.'
