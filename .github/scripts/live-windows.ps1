# Runs the real oku.exe on Windows against real GitHub releases, in throwaway
# directories. Each check throws on failure, which fails the job.
$ErrorActionPreference = 'Stop'

$root = Join-Path $env:RUNNER_TEMP 'oku-live'
$env:XDG_CONFIG_HOME = Join-Path $root 'config'
$env:XDG_DATA_HOME = Join-Path $root 'data'
$env:XDG_CACHE_HOME = Join-Path $root 'cache'
$oku = Join-Path $root 'oku.exe'

New-Item -ItemType Directory -Force $root | Out-Null
go build -o $oku ./cmd/oku
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }

function Check($what, [scriptblock]$test) {
    if (-not (& $test)) { throw "FAILED: $what" }
    Write-Host "ok: $what"
}

function Oku {
    & $oku @args
    if ($LASTEXITCODE -ne 0) { throw "oku $args exited with $LASTEXITCODE" }
}

$bin = Join-Path $env:XDG_DATA_HOME 'oku\profiles\global\current\bin'

Oku add github:BurntSushi/ripgrep
Check 'the profile has a shim and its spec file' {
    (Test-Path "$bin\rg.exe") -and (Test-Path "$bin\rg.shim")
}
Check 'current is a junction, not a symlink' {
    (Get-Item (Split-Path $bin)).LinkType -eq 'Junction'
}

$version = & "$bin\rg.exe" --version
Check 'the shim passes arguments and stdout through' { $version -match '^ripgrep \d' }

$found = 'needle in a haystack' | & "$bin\rg.exe" needle
Check 'the shim passes stdin through' { $found -match 'needle' }

'nothing here' | & "$bin\rg.exe" needle | Out-Null
Check 'the shim passes the exit code through' { $LASTEXITCODE -eq 1 }

Push-Location $env:RUNNER_TEMP
$elsewhere = & "$bin\rg.exe" --version
Pop-Location
Check 'the shim works from another directory' { $elsewhere -match '^ripgrep \d' }

Oku add github:sharkdp/fd
Check 'a second generation holds both programs' {
    (Test-Path "$bin\rg.exe") -and (Test-Path "$bin\fd.exe")
}

Oku sync
Oku update
Check 'sync and update keep both programs' {
    (Test-Path "$bin\rg.exe") -and (Test-Path "$bin\fd.exe")
}

Oku rollback
Check 'rollback takes fd away and keeps rg' {
    (Test-Path "$bin\rg.exe") -and -not (Test-Path "$bin\fd.exe")
}

$generations = & $oku generations
Check 'generations marks the active one' { @($generations | Where-Object { $_ -match '^\* ' }).Count -eq 1 }

Oku remove ripgrep
Check 'remove takes the shim away' { -not (Test-Path "$bin\rg.exe") }

Oku gc --keep 1
$listed = & $oku list
Check 'list no longer shows ripgrep' { ($listed -join "`n") -notmatch 'ripgrep' }

# The PowerShell hook, with a project that needs fd.
$project = Join-Path $root 'project'
New-Item -ItemType Directory -Force $project | Out-Null
Set-Content (Join-Path $project 'oku.toml') "[packages]`nfd = `"github:sharkdp/fd`"`n"
$env:PATH = "$root;$env:PATH"

Set-Location $project
Oku sync
Oku allow
Set-Location $root

Invoke-Expression ((& oku hook pwsh) -join [Environment]::NewLine)
Invoke-Expression ((& oku hook pwsh) -join [Environment]::NewLine)
Check 'the hook wrapped the prompt' { Test-Path Function:\_oku_prompt }

prompt | Out-Null
Check 'outside the project fd is not on PATH' { -not (Get-Command fd -ErrorAction SilentlyContinue) }

Set-Location $project
cmd /c exit 7
prompt | Out-Null
Check 'the prompt keeps LASTEXITCODE' { $LASTEXITCODE -eq 7 }
Check 'inside the project fd comes from the project profile' {
    (Get-Command fd).Source -like '*profiles*project-*'
}
$fdVersion = & fd --version
Check 'fd runs through its shim' { $fdVersion -match '^fd \d' }

Set-Location $root
prompt | Out-Null
Check 'leaving the project takes fd away again' { -not (Get-Command fd -ErrorAction SilentlyContinue) }

# Uninstall, which has to delete the running oku.exe and the junctions.
Set-Location $env:RUNNER_TEMP
Oku self uninstall --yes
Check 'oku.exe is no longer at its path' { -not (Test-Path $oku) }
Check 'data, cache and config are gone' {
    -not (Test-Path "$env:XDG_DATA_HOME\oku") -and -not (Test-Path "$env:XDG_CACHE_HOME\oku") -and
    -not (Test-Path "$env:XDG_CONFIG_HOME\oku")
}
Check 'the project list and lock are untouched' {
    (Test-Path "$project\oku.toml") -and (Test-Path "$project\oku.lock")
}
Start-Sleep -Seconds 8
Check 'the file that was moved aside is deleted once oku has exited' {
    -not (Test-Path "$oku.uninstalled")
}

Remove-Item -Recurse -Force $root
Write-Host 'live test passed'
