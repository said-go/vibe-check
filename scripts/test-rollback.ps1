$ErrorActionPreference = 'Stop'
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('vibe-rollback-test-' + [guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($fixture) | Out-Null
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'rollback-yandex.ps1') -Destination $fixture
[IO.File]::WriteAllText((Join-Path $fixture 'run-command.ps1'), @'
function Invoke-BoundedCommand { param($Executable,$Arguments,$TimeoutSeconds)
    $global:rollbackReads++
    if ($Executable -ne 'yc' -or ($Arguments[0..3] -join ' ') -ne 'serverless container revision list') { throw 'Unexpected cloud command' }
    ConvertTo-Json -InputObject @($global:rollbackRevision) -Depth 10 -Compress
}
'@)
[IO.File]::WriteAllText((Join-Path $fixture 'deploy-yandex.ps1'), @'
param($FolderId,$RegistryId,$ContainerId,$ServiceAccountId,$SecretId,$SecretVersionId,$YDBConnectionString,$YDBBudgetKey,$SharedAnalysesPerHour,$SharedAnalysesPerMonth,$ImageDigest,[switch]$Apply)
if ($Apply) { $global:rollbackDeploys++; $global:rollbackArguments=$PSBoundParameters }
'@)
$state = @{folderId='folder123';registryId='registry123';containerId='container123';serviceAccountId='account123';secretId='secret123';secretVersionId='version123';sharedHourly=5;sharedMonthly=100;budgetKey='budget-key';ydbConnectionString='grpcs://host:2135/?database=/database123'}
$statePath = Join-Path $fixture 'state.json'
$scriptPath = Join-Path $fixture 'rollback-yandex.ps1'
$digest = 'sha256:' + ('a'*64)
$global:rollbackReads=0; $global:rollbackDeploys=0
$checks=0
function Save-State { ConvertTo-Json -InputObject $state | Set-Content -LiteralPath $statePath -Encoding UTF8 }
function Reset-Revision {
    $global:rollbackRevision = @{
        status='ACTIVE';service_account_id='account123';resources=@{memory='1073741824';cores='1';core_fraction='100'};
        execution_timeout='180s';concurrency='2';scaling_policy=@{zone_instances_limit='1'};
        metadata_options=@{}; log_options=@{folder_id='folder123'}; runtime=@{http=@{}};
        secrets=@(@{id='secret123';version_id='version123';key='YANDEX_API_KEY';environment_variable='YANDEX_API_KEY'});
        image=@{image_url="cr.yandex/registry123/vibe-check@$digest";environment=@{
            APP_ENV='production';AI_PROVIDER='yandex';YANDEX_FOLDER_ID='folder123';YANDEX_MODEL='qwen3.6-35b-a3b';FRONTEND_DIR='/app/dist';
            ANALYSES_PER_HOUR='30';ANALYSES_PER_CLIENT_MINUTE='3';MAX_CONCURRENT_ANALYSES='2';YDB_CONNECTION_STRING=$state.ydbConnectionString;
            YDB_BUDGET_KEY='budget-key';SHARED_ANALYSES_PER_HOUR='5';SHARED_ANALYSES_PER_MONTH='100'
        }}
    }
}
function Reject([scriptblock]$Action) {
    $before=$global:rollbackDeploys
    $failed=$false
    try { & $Action } catch { $failed=$true }
    if (-not $failed -or $before -ne $global:rollbackDeploys) { throw 'Unsafe rollback accepted' }
}
try {
    Save-State; Reset-Revision
    & $scriptPath -StateFile $statePath -ImageDigest $digest
    if ($global:rollbackReads -ne 0 -or $global:rollbackDeploys -ne 0) { throw 'Plan accessed cloud' }; $checks++
    & $scriptPath -StateFile $statePath -ImageDigest $digest -CheckCurrent
    if ($global:rollbackReads -ne 1 -or $global:rollbackDeploys -ne 0) { throw 'Read-only check mutated cloud' }; $checks++
    & $scriptPath -StateFile $statePath -ImageDigest $digest -Apply
    if ($global:rollbackDeploys -ne 1 -or $global:rollbackArguments.SharedAnalysesPerHour -ne 5 -or $global:rollbackArguments.SharedAnalysesPerMonth -ne 100 -or $global:rollbackArguments.YDBBudgetKey -ne 'budget-key' -or $global:rollbackArguments.ImageDigest -ne $digest) { throw 'Rollback lost durable budget or digest' }; $checks++
    foreach ($field in @('sharedHourly','sharedMonthly','budgetKey','ydbConnectionString','secretVersionId')) {
        $saved=$state[$field]; $state.Remove($field); Save-State
        Reject { & $scriptPath -StateFile $statePath -ImageDigest $digest -Apply }; $checks++
        $state[$field]=$saved
    }
    Save-State
    Reject { & $scriptPath -StateFile $statePath -ImageDigest 'latest' -Apply }; $checks++
    foreach ($mutation in @(
        { $global:rollbackRevision.image.environment.SHARED_ANALYSES_PER_MONTH='200' },
        { $global:rollbackRevision.image.environment.YDB_BUDGET_KEY='new-budget' },
        { $global:rollbackRevision.image.environment.LOCAL_DIAGNOSTICS='true' },
        { $global:rollbackRevision.secrets[0].version_id='version999' },
        { $global:rollbackRevision.resources.memory='2147483648' },
        { $global:rollbackRevision.status='OBSOLETE' },
        { $global:rollbackRevision.provision_policy=@{min_instances=1} },
        { $global:rollbackRevision.metadata_options=@{aws_v1_http_token='DISABLED'} },
        { $global:rollbackRevision.log_options=@{disabled=$true} },
        { $global:rollbackRevision.runtime=@{task=@{}} },
        { $global:rollbackRevision.image.image_url='cr.yandex/other/vibe-check:latest' }
    )) {
        Reset-Revision; & $mutation
        Reject { & $scriptPath -StateFile $statePath -ImageDigest $digest -Apply }; $checks++
    }
    Write-Host "Rollback checks passed: $checks. No cloud mutations or secret values."
} finally {
    $resolved=[IO.Path]::GetFullPath($fixture)
    $tempRoot=[IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    if (-not $resolved.StartsWith($tempRoot,[StringComparison]::OrdinalIgnoreCase) -or (Split-Path $resolved -Leaf) -notlike 'vibe-rollback-test-*') { throw 'Unexpected fixture cleanup path' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
