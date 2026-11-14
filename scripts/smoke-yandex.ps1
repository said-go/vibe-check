param(
    [Parameter(Mandatory)][string]$BaseUrl,
    [switch]$AllowHttp
)

$ErrorActionPreference = 'Stop'
$uri = $null
if (-not [Uri]::TryCreate($BaseUrl, [UriKind]::Absolute, [ref]$uri) -or $uri.UserInfo -or $uri.Query -or $uri.Fragment -or $uri.AbsolutePath -ne '/') { throw 'BaseUrl must be an absolute origin URL without credentials, path, query, or fragment.' }
if ($uri.Scheme -ne 'https' -and -not ($AllowHttp -and $uri.Scheme -eq 'http')) { throw 'Use HTTPS, or -AllowHttp for a local test server.' }
$origin = $uri.GetLeftPart([UriPartial]::Authority).TrimEnd('/')
$assets = @()
$iconPaths = @()
function Read-ResourceText($Response) {
    if ($Response.RawContentStream) {
        $Response.RawContentStream.Position = 0
        $reader = New-Object System.IO.StreamReader($Response.RawContentStream, [Text.Encoding]::UTF8)
        try { return $reader.ReadToEnd() } finally { $reader.Dispose() }
    }
    if ($Response.Content -is [byte[]]) { return [Text.Encoding]::UTF8.GetString($Response.Content) }
    return [string]$Response.Content
}

foreach ($check in @(
    @{ Path = '/api/healthz'; Type = 'application/json' },
    @{ Path = '/api/readyz'; Type = 'application/json' },
    @{ Path = '/'; Type = 'text/html' },
    @{ Path = '/manifest.webmanifest'; Type = '' }
)) {
    $url = "$origin$($check.Path)"
    try {
        $response = Invoke-WebRequest -Uri $url -Method Get -UseBasicParsing -TimeoutSec 15 -MaximumRedirection 0
    } catch {
        throw "Smoke check failed for $($check.Path): $($_.Exception.Message)"
    }
    if ($response.StatusCode -ne 200) { throw "Unexpected status $($response.StatusCode) for $($check.Path)." }
    if ($check.Type -and $response.Headers['Content-Type'] -notlike "$($check.Type)*") { throw "Unexpected Content-Type for $($check.Path)." }
    if ($check.Path -eq '/api/readyz') {
        try { $ready = $response.Content | ConvertFrom-Json -ErrorAction Stop }
        catch { throw 'Readiness response is not valid JSON.' }
        if ($ready.status -ne 'ready') { throw 'Application is not ready.' }
        if ($ready.ai_available -ne $true) { throw 'Application can serve private presets, but AI is not configured. Complete AI configuration before live AI testing.' }
    }
    if ($check.Path -eq '/manifest.webmanifest') {
        try { $manifest = $response.Content | ConvertFrom-Json -ErrorAction Stop }
        catch { throw 'PWA manifest is not valid JSON.' }
        if (-not $manifest.name -or -not $manifest.icons) { throw 'PWA manifest is incomplete.' }
        foreach ($icon in $manifest.icons) {
            if ($icon.src -notmatch '^/?icons/[a-zA-Z0-9_-]+\.png$') { throw 'Unsafe manifest icon path.' }
            $path = '/' + $icon.src.TrimStart('/')
            $iconPaths += $path
            $resource = Invoke-WebRequest -Uri "$origin$path" -UseBasicParsing -TimeoutSec 15 -MaximumRedirection 0
            if ($resource.StatusCode -ne 200 -or $resource.Headers['Content-Type'] -notlike 'image/png*') { throw 'PWA icon is unavailable or has incorrect Content-Type.' }
        }
    }
    if ($check.Path -eq '/') {
        $assets = @([regex]::Matches($response.Content, '(?:src|href)="/?(assets/[a-z-]+\.[a-f0-9]{16}\.(?:js|css))"') | ForEach-Object { $_.Groups[1].Value } | Select-Object -Unique)
        if ($assets.Count -lt 4) { throw 'HTML does not reference a complete versioned PWA release.' }
        foreach ($asset in $assets) {
            $resource = Invoke-WebRequest -Uri "$origin/$asset" -UseBasicParsing -TimeoutSec 15 -MaximumRedirection 0
            $expectedType = if ($asset.EndsWith('.css')) { 'text/css' } else { '*javascript*' }
            if ($resource.StatusCode -ne 200 -or $resource.Headers['Content-Type'] -notlike "$expectedType*") { throw "PWA resource is unavailable or has incorrect Content-Type: $asset" }
            $text = (Read-ResourceText $resource).Replace("`r`n", "`n")
            $sha = [Security.Cryptography.SHA256]::Create()
            try { $digest = ([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($text)))).Replace('-', '').ToLowerInvariant().Substring(0,16) } finally { $sha.Dispose() }
            if ($asset -notmatch "\.$digest\.(js|css)$") { throw "PWA resource hash mismatch: $asset" }
            Write-Host "OK /$asset (200)"
        }
    }
    Write-Host "OK $($check.Path) (200)"
}
$worker = Invoke-WebRequest -Uri "$origin/sw.js" -UseBasicParsing -TimeoutSec 15 -MaximumRedirection 0
if ($worker.StatusCode -ne 200 -or $worker.Headers['Content-Type'] -notlike '*javascript*') { throw 'Service Worker is unavailable.' }
$match = [regex]::Match((Read-ResourceText $worker), 'const APP_SHELL = (\[[^;]+\]);')
if (-not $match.Success) { throw 'Service Worker shell manifest is missing.' }
try { $shell = $match.Groups[1].Value | ConvertFrom-Json -ErrorAction Stop } catch { throw 'Invalid Service Worker shell manifest.' }
$expected = @('/index.html','/manifest.webmanifest','/offline.html') + @($assets | ForEach-Object { '/' + $_ }) + $iconPaths
if ($shell.Count -ne $expected.Count -or @($shell | Select-Object -Unique).Count -ne $shell.Count -or @(Compare-Object $shell $expected).Count -ne 0) { throw 'Service Worker and HTML/manifest release mismatch.' }
Write-Host 'Smoke checks passed. Test the consent and AI result flow with a non-sensitive image before inviting users.'
