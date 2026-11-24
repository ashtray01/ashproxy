param(
    [Parameter(Mandatory)][string]$OutputDirectory,
    [string]$Version
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
if (!$Version) { $Version = (Get-Content -LiteralPath (Join-Path $root 'desktop/VERSION') -Raw).Trim() }
$output = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force $output | Out-Null
$stage = Join-Path ([IO.Path]::GetTempPath()) ("ashproxy-package-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $stage | Out-Null
try {
    $licenses = New-Item -ItemType Directory -Path (Join-Path $stage 'licenses')
    $core = New-Item -ItemType Directory -Path (Join-Path $stage 'core')
    Copy-Item -LiteralPath (Join-Path $root 'desktop/ashproxy.exe') -Destination $stage
    Copy-Item -LiteralPath (Join-Path $root 'desktop/VERSION') -Destination $stage
    Copy-Item -LiteralPath (Join-Path $root 'desktop/core/3proxy.exe') -Destination $core
    Copy-Item -LiteralPath (Join-Path $root 'desktop/README.ru.md') -Destination $stage
    Copy-Item -LiteralPath (Join-Path $root 'copying'),(Join-Path $root 'authors') -Destination $licenses
    Push-Location (Join-Path $root 'desktop')
    try {
        $modules = go list -deps -f '{{with .Module}}{{if not .Main}}{{.Dir}}{{end}}{{end}}' . | Sort-Object -Unique
        if ($LASTEXITCODE) {throw 'go list failed'}
        foreach ($module in $modules) {
            if (!$module) {continue}
            $name = (Split-Path $module -Leaf) + '-' + (Split-Path (Split-Path $module -Parent) -Leaf)
            $file = Get-ChildItem -LiteralPath $module -File | Where-Object Name -match '^LICEN[CS]E(\..*)?$' | Select-Object -First 1
            if (!$file) {throw "Missing license for $module"}
            Copy-Item -LiteralPath $file.FullName -Destination (Join-Path $licenses ($name + '.txt'))
        }
        Copy-Item -LiteralPath (Join-Path (go env GOROOT) 'LICENSE') -Destination (Join-Path $licenses 'Go.txt')
    } finally {Pop-Location}
    $archive = Join-Path $output ('ashproxy-' + $Version.Trim() + '-windows-amd64.zip')
    Compress-Archive -Path (Join-Path $stage '*') -DestinationPath $archive -Force
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant()
    Set-Content -Encoding ascii -LiteralPath (Join-Path $output 'SHA256SUMS.txt') -Value ($hash + '  ' + (Split-Path $archive -Leaf))
    Get-Item -LiteralPath $archive
} finally {
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\')
    if ((Split-Path $stage -Parent) -eq $tempRoot -and (Split-Path $stage -Leaf) -match '^ashproxy-package-[0-9a-f-]{36}$') {
        Remove-Item -LiteralPath $stage -Recurse -Force
    }
}
