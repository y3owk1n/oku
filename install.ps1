# Installs oku for the current user. It needs no administrator rights and edits
# no existing file.
#
#   irm https://raw.githubusercontent.com/y3owk1n/oku/main/install.ps1 | iex
#
# $env:OKU_INSTALL_DIR  where the binary goes, default %LOCALAPPDATA%\oku\bin
# $env:OKU_VERSION      a release tag such as v0.1.0, or nightly for the build of
#                       the newest commit on main, default the newest release
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

$version = (& (Join-Path $dir 'oku.exe') --version 2>$null | Select-Object -First 1)
if (-not $version) { $version = 'oku' }
Write-Host "installed $version at $(Join-Path $dir 'oku.exe') after checking its sha256"

# The line uses $HOME when the binary is under it, so it also works in a profile
# that several machines share.
$oku = Join-Path $dir 'oku.exe'
if ($oku.StartsWith($HOME, [StringComparison]::OrdinalIgnoreCase)) { $oku = '$HOME' + $oku.Substring($HOME.Length) }
$line = "if (Test-Path `"$oku`") { Invoke-Expression ((& `"$oku`" hook pwsh) -join [Environment]::NewLine) }"

# What to do once oku is on PATH.
function Write-NextSteps {
    Write-Host ''
    Write-Host 'then:'
    Write-Host '  oku doctor                  checks the setup'
    Write-Host '  oku add github:sharkdp/fd   installs a first program, try: fd --version'
    Write-Host '  oku self update             replaces oku with the newest release later'
}

Write-Host ''

# A profile that loads the hook already, from an earlier install, needs no
# second line. PowerShell reads four profiles, one per scope and host, and the
# line works in any of them. The pattern is the one "oku doctor" uses.
$loaded = @(
    $PROFILE.AllUsersAllHosts, $PROFILE.AllUsersCurrentHost,
    $PROFILE.CurrentUserAllHosts, $PROFILE.CurrentUserCurrentHost
) | Where-Object { $_ -and (Test-Path $_) -and (Select-String -Path $_ -Pattern 'oku(\.exe)?" hook|oku hook' -Quiet) }

if ($loaded) {
    Write-Host "$($loaded[0]) already loads oku. Open a new terminal, or run:  . `$PROFILE"
    Write-NextSteps
    return
}

Write-Host 'There is one step left. Add this line to the file that $PROFILE names:'
Write-Host ''
Write-Host "  $line"
Write-Host ''
Write-Host 'It puts oku and the programs it installs on PATH, and loads its completions. This command adds it for you:'
Write-Host ''
Write-Host "  if (-not (Test-Path `$PROFILE)) { New-Item -Force -ItemType File `$PROFILE | Out-Null }; Add-Content `$PROFILE '$line'; . `$PROFILE"
Write-NextSteps
