$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$global:deployTestCount = 0
function Assert-Throws([scriptblock]$Action, [string]$Pattern) {
    try { & $Action; throw 'Expected failure was not raised' } catch {
        if ($_.Exception.Message -notlike "*$Pattern*") { throw }
    }
    $global:deployTestCount++
}
$ids = @{ FolderId='folder123'; RegistryId='registry123'; ContainerId='container123'; ServiceAccountId='account123'; SecretId='secret123'; SecretVersionId='version123' }
$global:deployTestMutations = 0
function git { $global:LASTEXITCODE=0; if ($args -contains 'rev-parse') { 'a' * 40 } else { ' M dirty' } }
function docker { $global:deployTestMutations++; throw 'Unexpected Docker call' }
function yc { $global:deployTestMutations++; throw 'Unexpected Cloud call' }
& "$PSScriptRoot/deploy-yandex.ps1" @ids
$global:deployTestCount++
Assert-Throws { & "$PSScriptRoot/deploy-yandex.ps1" @ids -Apply } 'Working tree must be clean'
$bad = $ids.Clone(); $bad.FolderId='invalid;command'
Assert-Throws { & "$PSScriptRoot/deploy-yandex.ps1" @bad } 'resource ID'
if ($global:deployTestMutations -ne 0) { throw 'Plan/validation unexpectedly invoked cloud tools.' }
Assert-Throws { & "$PSScriptRoot/deploy-yandex.ps1" @ids -RedisSecretId redis123 } 'supplied together'
Assert-Throws { & "$PSScriptRoot/deploy-yandex.ps1" @ids -RedisSecretVersionId version456 } 'supplied together'
Assert-Throws { & "$PSScriptRoot/deploy-yandex.ps1" @ids -SharedAnalysesPerHour 7 } 'require Redis secret'
Assert-Throws { & "$PSScriptRoot/deploy-yandex.ps1" @ids -RedisSecretId redis123 -RedisSecretVersionId version456 -RedisBudgetKey 'bad,key=value' } 'Invalid Redis budget'
Assert-Throws { & "$PSScriptRoot/deploy-yandex.ps1" @ids -RedisSecretId redis123 -RedisSecretVersionId version456 -SharedAnalysesPerHour 0 } 'Invalid Redis budget'
foreach ($invalidImage in @('', 'latest', ('a'*40), 'cr.yandex/other/vibe-check:latest', ('sha256:' + ('A'*64)), ('sha256:' + ('a'*64) + ';bad'))) {
    Assert-Throws { & "$PSScriptRoot/deploy-yandex.ps1" @ids -ImageDigest $invalidImage } 'immutable sha256'
}
& "$PSScriptRoot/deploy-yandex.ps1" @ids -ImageDigest ('sha256:' + ('b'*64))
$global:deployTestCount++
# Execute the actual deploy script against a temporary fake command runner.
# Never add a mock switch to the production deployment entry point.
$fixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ('vibe-deploy-test-' + [guid]::NewGuid().ToString('N'))
$fixtureScripts = Join-Path $fixtureRoot 'scripts'
[IO.Directory]::CreateDirectory($fixtureScripts) | Out-Null
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'deploy-yandex.ps1') -Destination $fixtureScripts
$fakeRunner = @'
function Invoke-BoundedCommand {
    param($Executable, [string[]]$Arguments, $TimeoutSeconds, $WorkingDirectory)
    $global:deployTestCommands.Add(@{ Tool=$Executable; Arguments=$Arguments })
    if ($global:deployTestFailGet -and $Arguments -contains 'get') { throw 'Target lookup failed' }
}
'@
[IO.File]::WriteAllText((Join-Path $fixtureScripts 'run-command.ps1'), $fakeRunner)
try {
    function git { $global:LASTEXITCODE=0; if ($args -contains 'rev-parse') { 'a' * 40 } }
    foreach ($withRedis in @($false, $true)) {
        $global:deployTestCommands = New-Object 'System.Collections.Generic.List[object]'
        $options=$ids.Clone()
        if($withRedis) { $options.RedisSecretId='redis123'; $options.RedisSecretVersionId='version456'; $options.RedisBudgetKey='test:budget'; $options.SharedAnalysesPerHour=7 }
        & (Join-Path $fixtureScripts 'deploy-yandex.ps1') @options -Apply
        if($global:deployTestCommands.Count -ne 5){throw 'Unexpected deploy command count'}
        $revision=$global:deployTestCommands[4]
        $argv=$revision.Arguments
        if($revision.Tool -ne 'yc' -or ($argv[0..3] -join ' ') -ne 'serverless container revision deploy'){throw 'Wrong deploy command'}
        $secretFlags=@($argv | Where-Object { $_ -eq '--secret' }).Count
        if($secretFlags -ne (1+[int]$withRedis)){throw 'Wrong secret bindings count'}
        $environmentArg=$argv[[array]::IndexOf($argv,'--environment')+1]
        if($withRedis) {
            if($argv -notcontains 'environment-variable=REDIS_BUDGET_URL,id=redis123,version-id=version456,key=REDIS_BUDGET_URL'){throw 'Redis secret binding missing'}
            if($environmentArg -notlike '*REDIS_BUDGET_KEY=test:budget,SHARED_ANALYSES_PER_HOUR=7'){throw 'Redis budget settings missing'}
        } elseif(($argv -join ' ') -match 'REDIS_|SHARED_ANALYSES'){throw 'Default deploy unexpectedly enables Redis'}
        if($environmentArg -match 'REDIS_BUDGET_URL' -or ($argv -join ' ') -match 'redis[s]?://|allow-unauthenticated'){throw 'Unexpected plaintext URL or public access mutation'}
        $global:deployTestCount++
    }
    $global:deployTestCommands = New-Object 'System.Collections.Generic.List[object]'
    $digest='sha256:' + ('b'*64)
    & (Join-Path $fixtureScripts 'deploy-yandex.ps1') @ids -ImageDigest $digest -RedisSecretId redis123 -RedisSecretVersionId version456 -Apply
    if ($global:deployTestCommands.Count -ne 2 -or @($global:deployTestCommands | Where-Object { $_.Tool -ne 'yc' }).Count -ne 0) { throw 'Rollback must only get target and deploy revision' }
    $argv=$global:deployTestCommands[1].Arguments
    if ($argv[[array]::IndexOf($argv,'--image')+1] -cne "cr.yandex/registry123/vibe-check@$digest") { throw 'Rollback digest changed' }
    if (@($argv | Where-Object { $_ -eq '--secret' }).Count -ne 2) { throw 'Rollback dropped configured secrets' }
    $global:deployTestCount++
    $global:deployTestCommands = New-Object 'System.Collections.Generic.List[object]'
    $global:deployTestFailGet=$true
    Assert-Throws { & (Join-Path $fixtureScripts 'deploy-yandex.ps1') @ids -ImageDigest $digest -Apply } 'Target lookup failed'
    if ($global:deployTestCommands.Count -ne 1) { throw 'Rollback continued after lookup failure' }
    $global:deployTestFailGet=$false
} finally {
    Remove-Item -LiteralPath (Join-Path $fixtureScripts 'deploy-yandex.ps1'),(Join-Path $fixtureScripts 'run-command.ps1')
    [IO.Directory]::Delete($fixtureScripts)
    [IO.Directory]::Delete($fixtureRoot)
}
$global:deployTestRequests = 0
$global:deployTestAI = $true
$global:deployTestBrokenAsset = $false
function Invoke-WebRequest {
    param($Uri,$Method,[switch]$UseBasicParsing,$TimeoutSec,$MaximumRedirection)
    $global:deployTestRequests++
    $path = ([Uri]$Uri).AbsolutePath
    $content = '{}'; $type = 'application/json'
    switch ($path) {
        '/api/readyz' { $content = @{status='ready';ai_available=$global:deployTestAI} | ConvertTo-Json }
        '/' { $type='text/html'; $content=[IO.File]::ReadAllText((Join-Path $root 'dist/index.html')) }
        '/manifest.webmanifest' { $content=[IO.File]::ReadAllText((Join-Path $root 'dist/manifest.webmanifest')); if($global:deployTestUnsafeIcon){$content=$content.Replace('icons/icon-192.png','https://example.invalid/evil.png')} }
        '/sw.js' { $type='application/javascript'; $content=[IO.File]::ReadAllText((Join-Path $root 'dist/sw.js')); if($global:deployTestBrokenSW){$content=$content.Replace('/index.html','/stale.html')} }
        default {
            if ($path -like '/assets/*') {
                $type=if($path.EndsWith('.css')){'text/css'}else{'application/javascript'}
                $content=[IO.File]::ReadAllText((Join-Path $root ('dist'+$path))).Replace("`r`n","`n").Replace("`n","`r`n")
                if($global:deployTestBrokenAsset){$type='text/html'}
                if($global:deployTestCorruptHash){$content+='corrupt'}
            } elseif($path -like '/icons/*') { $type=if($global:deployTestBrokenIcon){'text/html'}else{'image/png'} }
        }
    }
    @{StatusCode=200;Headers=@{'Content-Type'=$type};Content=$content}
}
Assert-Throws { & "$PSScriptRoot/smoke-yandex.ps1" -BaseUrl 'https://example.invalid/path' } 'origin URL'
if($global:deployTestRequests -ne 0){throw 'Invalid URL was fetched'}
& "$PSScriptRoot/smoke-yandex.ps1" -BaseUrl 'https://example.invalid/'
$global:deployTestCount++
$global:deployTestAI=$false
Assert-Throws { & "$PSScriptRoot/smoke-yandex.ps1" -BaseUrl 'https://example.invalid/' } 'AI is not configured'
$global:deployTestAI=$true; $global:deployTestBrokenAsset=$true
Assert-Throws { & "$PSScriptRoot/smoke-yandex.ps1" -BaseUrl 'https://example.invalid/' } 'incorrect Content-Type'
$global:deployTestBrokenAsset=$false
$global:deployTestCorruptHash=$true
Assert-Throws { & "$PSScriptRoot/smoke-yandex.ps1" -BaseUrl 'https://example.invalid/' } 'hash mismatch'
$global:deployTestCorruptHash=$false; $global:deployTestBrokenSW=$true
Assert-Throws { & "$PSScriptRoot/smoke-yandex.ps1" -BaseUrl 'https://example.invalid/' } 'release mismatch'
$global:deployTestBrokenSW=$false; $global:deployTestUnsafeIcon=$true
Assert-Throws { & "$PSScriptRoot/smoke-yandex.ps1" -BaseUrl 'https://example.invalid/' } 'Unsafe manifest icon'
$global:deployTestUnsafeIcon=$false; $global:deployTestBrokenIcon=$true
Assert-Throws { & "$PSScriptRoot/smoke-yandex.ps1" -BaseUrl 'https://example.invalid/' } 'icon is unavailable'
$global:deployTestBrokenIcon=$false
. "$PSScriptRoot/run-command.ps1"
$shell = (Get-Process -Id $PID).Path
$output = Invoke-BoundedCommand $shell @('-NoProfile','-Command','Write-Output ''bounded ok''') 10
if($output.Trim() -ne 'bounded ok'){throw 'Command output mismatch'}
$global:deployTestCount++
Assert-Throws { Invoke-BoundedCommand $shell @('-NoProfile','-Command','exit 7') 10 } 'exit code 7'
Assert-Throws { Invoke-BoundedCommand $shell @('-NoProfile','-Command','Start-Sleep -Seconds 15') 1 } 'deadline'
Write-Host "Deployment regression checks passed: $global:deployTestCount. No cloud calls or photo uploads."
