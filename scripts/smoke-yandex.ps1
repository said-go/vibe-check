param(
    [Parameter(Mandatory)][string]$BaseUrl,
    [switch]$AllowHttp
)

$ErrorActionPreference = 'Stop'
$uri = $null
if (-not [Uri]::TryCreate($BaseUrl, [UriKind]::Absolute, [ref]$uri) -or $uri.UserInfo -or $uri.Query -or $uri.Fragment -or $uri.AbsolutePath -ne '/') { throw 'BaseUrl must be an absolute origin URL without credentials, path, query, or fragment.' }
if ($uri.Scheme -ne 'https' -and -not ($AllowHttp -and $uri.Scheme -eq 'http')) { throw 'Use HTTPS, or -AllowHttp for a local test server.' }
$origin = $uri.GetLeftPart([UriPartial]::Authority).TrimEnd('/')

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
    }
    Write-Host "OK $($check.Path) (200)"
}
Write-Host 'Smoke checks passed. Test the consent and AI result flow with a non-sensitive image before inviting users.'
