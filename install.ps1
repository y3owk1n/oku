# Installs oku for the current user. It needs no administrator rights and edits
# no existing file.
#
#   irm https://raw.githubusercontent.com/y3owk1n/oku/main/install.ps1 | iex
#
# $env:OKU_INSTALL_DIR  where the binary goes, default %LOCALAPPDATA%\oku\bin
# $env:OKU_VERSION      a release tag such as v0.1.0, default the newest release
$ErrorActionPreference = 'Stop'

$repo = 'y3owk1n/oku'
$dir = if ($env:OKU_INSTALL_DIR) { $env:OKU_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'oku\bin' }
$base = if ($env:OKU_RELEASE_URL) { $env:OKU_RELEASE_URL } else { "https://github.com/$repo/releases" }
$from = if ($env:OKU_VERSION) { "$base/download/$env:OKU_VERSION" } else { "$base/latest/download" }

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$name = "oku-windows-$arch.exe"

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("oku-install-" + [Guid]::NewGuid())
New-Item -ItemType Directory $tmp | Out-Null

try {
    Invoke-WebRequest "$from/$name" -OutFile (Join-Path $tmp $name) -UseBasicParsing
    Invoke-WebRequest "$from/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing

    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { ($_ -split '\s+')[1] -eq $name }
    if (-not $line) { throw "checksums.txt does not list $name" }

    $want = ($line -split '\s+')[0]
    $got = (Get-FileHash (Join-Path $tmp $name) -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $want) { throw "the sha256 of $name is $got, and checksums.txt says $want" }

    New-Item -ItemType Directory -Force $dir | Out-Null
    Move-Item -Force (Join-Path $tmp $name) (Join-Path $dir 'oku.exe')
}
finally {
    Remove-Item -Recurse -Force $tmp
}

Write-Host "installed $(Join-Path $dir 'oku.exe') after checking its sha256"

if (($env:PATH -split ';') -notcontains $dir) {
    Write-Host "add $dir to PATH to run it"
}

Write-Host 'to use oku in projects, add this line to the file that $PROFILE names:'
Write-Host "  if (Get-Command oku -ErrorAction SilentlyContinue) { Invoke-Expression ((& oku hook pwsh) -join [Environment]::NewLine) }"
