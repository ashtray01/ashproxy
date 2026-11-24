# Builds desktop/assets/app.png, app.ico and the Windows icon and version
# resource of the executable from the project picture. The picture is used as
# drawn; pass -Transparent to drop a flat white background first. The version
# and the publisher details come from desktop/VERSION and the block below.
# Usage: powershell -File desktop/tools/make-icon.ps1 [-Source <png>] [-Transparent]
param(
    [string]$Source = (Join-Path $PSScriptRoot '..\..\..\icon.png'),
    [string]$Png = (Join-Path $PSScriptRoot '..\assets\app.png'),
    [string]$Ico = (Join-Path $PSScriptRoot '..\assets\app.ico'),
    [string]$Syso = (Join-Path $PSScriptRoot '..\rsrc_windows_amd64.syso'),
    [string]$Version = (Get-Content -LiteralPath (Join-Path $PSScriptRoot '..\VERSION') -Raw),
    [switch]$Transparent
)
$ErrorActionPreference = 'Stop'
if (!(Test-Path -LiteralPath $Source)) { throw "Missing source picture: $Source" }
Add-Type -TypeDefinition (Get-Content -LiteralPath (Join-Path $PSScriptRoot 'icongen.cs') -Raw) `
    -ReferencedAssemblies System.Drawing -Language CSharp
$pngPath = [IO.Path]::GetFullPath($Png)
$icoPath = [IO.Path]::GetFullPath($Ico)
$sourcePath = (Resolve-Path -LiteralPath $Source).Path
if ($Transparent) { [IconGen]::Run($sourcePath, $pngPath, $icoPath) }
else { [IconGen]::RunFlat($sourcePath, $pngPath, $icoPath) }

$Version = $Version.Trim()
if ($Version -notmatch '^\d+(\.\d+){0,3}$') { throw "VERSION must look like 1.0.0, got '$Version'" }
$parts = @($Version.Split('.') | ForEach-Object { [int]$_ })
while ($parts.Count -lt 4) { $parts += 0 }
$numeric = $parts[0..3] -join ','

$windres = (Get-Command windres -ErrorAction SilentlyContinue).Source
if ($windres) {
    $rc = Join-Path ([IO.Path]::GetTempPath()) ('ashproxy-icon-' + [guid]::NewGuid() + '.rc')
    try {
        $body = @"
1 VERSIONINFO
FILEVERSION $numeric
PRODUCTVERSION $numeric
FILEFLAGSMASK 0x3fL
FILEFLAGS 0x0L
FILEOS 0x40004L
FILETYPE 0x1L
FILESUBTYPE 0x0L
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "040904b0"
    BEGIN
      VALUE "CompanyName", "AshSoft Ltd."
      VALUE "FileDescription", "AshProxy proxy server for Windows"
      VALUE "FileVersion", "$Version"
      VALUE "InternalName", "ashproxy"
      VALUE "LegalCopyright", "Copyright (c) 2026 AshSoft Ltd. Owner: ashtray, ashtray@icloud.com"
      VALUE "OriginalFilename", "ashproxy.exe"
      VALUE "ProductName", "AshProxy"
      VALUE "ProductVersion", "$Version"
      VALUE "Comments", "Publisher: AshSoft Ltd. Contact: ashtray@icloud.com"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x409, 1200
  END
END
3 ICON "$($icoPath -replace '\\', '/')"
"@
        Set-Content -LiteralPath $rc -Encoding ascii -Value $body
        & $windres --target=pe-x86-64 -O coff -o ([IO.Path]::GetFullPath($Syso)) $rc
        if ($LASTEXITCODE) { throw "windres failed with $LASTEXITCODE" }
        "wrote $Syso (version $Version)"
    } finally {
        Remove-Item -LiteralPath $rc -Force -ErrorAction SilentlyContinue
    }
} else {
    'windres not found: the executable icon and version resource was not rebuilt'
}
