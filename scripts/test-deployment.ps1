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
        '/' { $type='text/html'; $content='<script src="assets/app.aaaaaaaaaaaaaaaa.js"></script><script src="assets/history.aaaaaaaaaaaaaaaa.js"></script><script src="assets/share-card.aaaaaaaaaaaaaaaa.js"></script><link href="assets/styles.aaaaaaaaaaaaaaaa.css">' }
        '/manifest.webmanifest' { $content='{"name":"test","icons":[{}]}' }
        default { if ($path -like '/assets/*') { $type=if($path.EndsWith('.css')){'text/css'}else{'application/javascript'}; if($global:deployTestBrokenAsset){$type='text/html'} } }
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
. "$PSScriptRoot/run-command.ps1"
$shell = (Get-Process -Id $PID).Path
$output = Invoke-BoundedCommand $shell @('-NoProfile','-Command','Write-Output ''bounded ok''') 10
if($output.Trim() -ne 'bounded ok'){throw 'Command output mismatch'}
$global:deployTestCount++
Assert-Throws { Invoke-BoundedCommand $shell @('-NoProfile','-Command','exit 7') 10 } 'exit code 7'
Assert-Throws { Invoke-BoundedCommand $shell @('-NoProfile','-Command','Start-Sleep -Seconds 15') 1 } 'deadline'
Write-Host "Deployment regression checks passed: $global:deployTestCount. No cloud calls or photo uploads."
