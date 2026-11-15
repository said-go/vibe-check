param(
    [Parameter(Mandatory)][string]$StateFile,
    [Parameter(Mandatory)][string]$ImageDigest,
    [switch]$CheckCurrent,
    [switch]$Apply
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'run-command.ps1')
if ($ImageDigest -cnotmatch '^sha256:[0-9a-f]{64}$') { throw 'An explicit immutable ImageDigest is required.' }
try { $state = Get-Content -LiteralPath $StateFile -Raw | ConvertFrom-Json } catch { throw 'Cannot read deployment state JSON.' }
$options = @{}
foreach ($name in @('FolderId','RegistryId','ContainerId','ServiceAccountId','SecretId','SecretVersionId')) {
    $value = $state.$name
    if ($value -isnot [string] -or $value -notmatch '^[a-zA-Z0-9_-]{6,80}$') { throw "Missing or invalid state field: $name" }
    $options[$name] = $value
}
foreach ($name in @('sharedHourly','sharedMonthly')) {
    $value = $state.$name
    if (($value -isnot [int] -and $value -isnot [long]) -or $value -lt 1 -or $value -gt 1000000) { throw "Missing or invalid state field: $name" }
}
if ($state.budgetKey -isnot [string] -or $state.budgetKey -notmatch '^[a-zA-Z0-9:_-]{1,128}$') { throw 'Missing or invalid state field: budgetKey' }
if ($state.ydbConnectionString -isnot [string] -or $state.ydbConnectionString -cnotmatch '^grpcs://[a-zA-Z0-9.-]+:[0-9]{1,5}/\?database=/[a-zA-Z0-9/_-]+$') { throw 'Missing or invalid state field: ydbConnectionString' }
$options.YDBConnectionString = $state.ydbConnectionString
$options.YDBBudgetKey = $state.budgetKey
$options.SharedAnalysesPerHour = [int]$state.sharedHourly
$options.SharedAnalysesPerMonth = [int]$state.sharedMonthly
$options.ImageDigest = $ImageDigest

# The deploy entry point validates the full DSN and all arguments without cloud calls.
& (Join-Path $PSScriptRoot 'deploy-yandex.ps1') @options
if (-not $Apply -and -not $CheckCurrent) { return }

# Reject configuration drift instead of silently replacing settings with defaults.
$raw = Invoke-BoundedCommand yc @('serverless','container','revision','list','--container-id',$state.containerId,'--folder-id',$state.folderId,'--format','json') 60
try { $revisions = @($raw | ConvertFrom-Json) } catch { throw 'Cannot parse current revisions.' }
$active = @($revisions | Where-Object { $_.status -eq 'ACTIVE' })
if ($active.Count -ne 1) { throw 'Expected exactly one active revision; inspect cloud state.' }
$revision = $active[0]
$expected = @{
    APP_ENV='production'; AI_PROVIDER='yandex'; YANDEX_FOLDER_ID=$state.folderId;
    YANDEX_MODEL='qwen3.6-35b-a3b'; FRONTEND_DIR='/app/dist'; ANALYSES_PER_HOUR='30';
    ANALYSES_PER_CLIENT_MINUTE='3'; MAX_CONCURRENT_ANALYSES='2';
    YDB_CONNECTION_STRING=$state.ydbConnectionString; YDB_BUDGET_KEY=$state.budgetKey;
    SHARED_ANALYSES_PER_HOUR=[string]$state.sharedHourly; SHARED_ANALYSES_PER_MONTH=[string]$state.sharedMonthly
}
$actualNames = @($revision.image.environment.PSObject.Properties.Name)
if ($actualNames.Count -ne $expected.Count) { throw 'Current environment differs from the supported rollback profile.' }
foreach ($name in $expected.Keys) {
    if ([string]$revision.image.environment.$name -cne $expected[$name]) { throw "Current configuration differs: $name. No revision was deployed." }
}
if ($revision.service_account_id -ne $state.serviceAccountId -or
    [string]$revision.resources.memory -ne '1073741824' -or [string]$revision.resources.cores -ne '1' -or
    [string]$revision.resources.core_fraction -ne '100' -or $revision.execution_timeout -ne '180s' -or
    [string]$revision.concurrency -ne '2' -or [string]$revision.scaling_policy.zone_instances_limit -ne '1' -or
    $revision.connectivity -or $revision.provision_policy) { throw 'Current runtime settings differ from the supported rollback profile.' }
if (@($revision.metadata_options.PSObject.Properties.Name | Where-Object { $_ }).Count -gt 0 -or
    @($revision.log_options.PSObject.Properties.Name).Count -ne 1 -or $revision.log_options.folder_id -ne $state.folderId -or
    @($revision.scaling_policy.PSObject.Properties.Name).Count -ne 1 -or
    @($revision.runtime.PSObject.Properties.Name).Count -ne 1 -or -not $revision.runtime.http -or
    @($revision.runtime.http.PSObject.Properties.Name | Where-Object { $_ }).Count -gt 0) { throw 'Current metadata, logging or runtime mode differs from the supported rollback profile.' }
$secrets = @($revision.secrets)
if ($secrets.Count -ne 1 -or $secrets[0].id -ne $state.secretId -or $secrets[0].version_id -ne $state.secretVersionId -or
    $secrets[0].key -ne 'YANDEX_API_KEY' -or $secrets[0].environment_variable -ne 'YANDEX_API_KEY') { throw 'Current secret references differ from deployment state.' }
if ($revision.image.image_url -notlike "cr.yandex/$($state.registryId)/vibe-check@sha256:*") { throw 'Current image repository differs from deployment state.' }
Write-Host 'Current revision matches the saved budget, secret and runtime profile.'
if ($Apply) { & (Join-Path $PSScriptRoot 'deploy-yandex.ps1') @options -Apply }
