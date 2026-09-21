# Runs the real oku.exe on Windows against real releases on GitHub, GitLab and
# gitea.com, in throwaway directories. Each check throws on failure, which fails the job.
$ErrorActionPreference = 'Stop'
$repoRoot = (Get-Location).Path

$root = Join-Path $env:RUNNER_TEMP 'oku-live'
$env:XDG_CONFIG_HOME = Join-Path $root 'config'
$env:XDG_DATA_HOME = Join-Path $root 'data'
$env:XDG_CACHE_HOME = Join-Path $root 'cache'
$oku = Join-Path $root 'oku.exe'

New-Item -ItemType Directory -Force $root | Out-Null
go build -o $oku ./cmd/oku
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }

# The checkout has an oku.toml, which would make every command below act on
# that project and not on the global profile.
Set-Location $root

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

$asJson = (& $oku list --json) -join "`n" | ConvertFrom-Json
Check 'list --json parses, and names both packages with their store paths' {
    ($asJson.Count -eq 2) -and ($asJson[0].name -eq 'fd') -and
    ($asJson[1].store_path -like '*store*ripgrep-*')
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

# doctor, without the profile on PATH and then with it. The hook above already
# put it there, so the first check takes it out again.
$withProfile = $env:PATH
$env:PATH = (($env:PATH -split ';') | Where-Object { $_ -ne $bin }) -join ';'
$diagnosis = (& $oku doctor) -join "`n"
Check 'doctor says that the profile is not on PATH, and exits with 1' {
    ($LASTEXITCODE -eq 1) -and ($diagnosis -match 'is not on PATH') -and
    ($diagnosis -match 'without a sandbox')
}
$env:PATH = $withProfile
Check 'the hook put the profile and oku on PATH' {
    (($env:PATH -split ';') -contains $bin) -and (($env:PATH -split ';') -contains $root)
}
$diagnosis = (& $oku doctor) -join "`n"
Check 'doctor finds no problem once the profile is on PATH' {
    ($LASTEXITCODE -eq 0) -and ($diagnosis -match 'is on PATH') -and
    ($diagnosis -match 'points at a file in the store')
}

# A build from source: a dep, a needs tool, pwsh and cmd steps, and an install.
$fixtures = Join-Path $root 'fixtures'
New-Item -ItemType Directory -Force $fixtures | Out-Null
$greetRef = (Join-Path $fixtures 'greet.toml') -replace '\\', '/'

Set-Content (Join-Path $fixtures 'greet.toml') @'
[package]
name = "greet"
[version]
value = "1.0.0"
[build]
[[build.step]]
run = "New-Item -ItemType Directory -Force '{{prefix}}/share' | Out-Null; Set-Content '{{prefix}}/share/greeting.txt' 'hello from a dep'"
shell = "pwsh"
'@

Set-Content (Join-Path $fixtures 'main.go') @'
package main

import (
	"fmt"
	"os"
)

func main() {
	text, _ := os.ReadFile(os.Args[1])
	fmt.Print(string(text))
}
'@
$mainGo = (Join-Path $fixtures 'main.go') -replace '\\', '/'

# A patch step changes what the program prints.
Set-Content (Join-Path $fixtures 'greet.patch') @'
diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -8,3 +8,3 @@
 func main() {
 	text, _ := os.ReadFile(os.Args[1])
-	fmt.Print(string(text))
+	fmt.Print("patched: " + string(text))
 }
'@
$patchFile = (Join-Path $fixtures 'greet.patch') -replace '\\', '/'

Set-Content (Join-Path $fixtures 'hello.toml') @"
[package]
name = "hello"
[version]
value = "1.0.0"
[build]
needs = ["go"]
deps = [{ ref = "$greetRef" }]
[[build.step]]
run = "Copy-Item '$mainGo' main.go; Set-Content go.mod 'module hello'; Copy-Item (Join-Path `$env:SystemRoot 'Fonts/arial.ttf') OkuLive.ttf"
shell = "pwsh"
[[build.step]]
run = "Copy-Item '$patchFile' greet.patch"
shell = "pwsh"
[[build.step]]
patch = { file = "greet.patch" }
[[build.step]]
run = "echo home=%USERPROFILE% > where.txt && go build -o hello.exe ."
shell = "cmd"
[[build.step]]
install = { bin = ["hello.exe"], share = ["where.txt"], font = ["OkuLive.ttf"] }
[[app]]
name = "Oku Hello"
exec = "bin/hello.exe"
"@

Set-Location $root
Oku add (Join-Path $fixtures 'hello.toml') --yes --verbose
$shimSpec = Get-Content "$bin\hello.shim"
Check 'the shim lists the bin directory of the dep' {
    ($shimSpec -join "`n") -match 'dir = .*greet-1\.0\.0-.*bin'
}

$greeting = Get-ChildItem "$env:XDG_DATA_HOME\oku\store\greet-*\share\greeting.txt"
$said = & "$bin\hello.exe" $greeting.FullName
Check 'the built program runs, reads the dep, and has the patch' {
    ($said -join ' ') -match 'patched: hello from a dep'
}

$where = Get-Content (Get-ChildItem "$env:XDG_DATA_HOME\oku\store\hello-*\share\where.txt").FullName
Check 'the build saw a scratch home, not the real profile' {
    ($where -match 'oku-build-') -and ($where -notmatch [regex]::Escape($env:USERPROFILE))
}

# The app and the font of that package, for the current user.
$shortcut = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\oku-oku-hello.lnk'
$font = Join-Path $env:LOCALAPPDATA 'Microsoft\Windows\Fonts\OkuLive.ttf'
$fontsKey = 'HKCU:\Software\Microsoft\Windows NT\CurrentVersion\Fonts'

Check 'the Start Menu has a shortcut to the program in the store' {
    (Test-Path $shortcut) -and
    ((New-Object -ComObject WScript.Shell).CreateShortcut($shortcut).TargetPath -like '*store*hello-1.0.0-*hello.exe')
}
Check 'the font is in the user font folder and in the registry' {
    (Test-Path $font) -and ((Get-ItemProperty $fontsKey).'oku OkuLive.ttf' -eq $font)
}

Oku remove hello
Check 'remove takes the shortcut, the font and its registry value away' {
    -not (Test-Path $shortcut) -and -not (Test-Path $font) -and
    -not ((Get-ItemProperty $fontsKey).PSObject.Properties.Name -contains 'oku OkuLive.ttf')
}

# The package comes back, so that uninstall has an app and a font to remove.
Oku add (Join-Path $fixtures 'hello.toml') --yes
Check 'adding the package again brings the shortcut and the font back' {
    (Test-Path $shortcut) -and (Test-Path $font)
}

# A service, which oku runs as a scheduled task of the current user.
Set-Content (Join-Path $fixtures 'ticker.go') @'
package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	fmt.Println("started with PORT=" + os.Getenv("PORT") + " and " + os.Args[1])
	time.Sleep(10 * time.Minute)
}
'@
$tickerGo = (Join-Path $fixtures 'ticker.go') -replace '\\', '/'

Set-Content (Join-Path $fixtures 'ticker.toml') @"
[package]
name = "ticker"
[version]
value = "1.0.0"
[build]
needs = ["go"]
[[build.step]]
run = "Copy-Item '$tickerGo' main.go; Set-Content go.mod 'module ticker'; go build -o ticker.exe ."
shell = "pwsh"
[[build.step]]
install = { bin = ["ticker.exe"] }
[[service]]
name = "ticker"
command = "bin/ticker.exe"
args = ["--data"]
env = { PORT = "8080" }
restart = "on-failure"
"@

function ServiceStatus { (& $oku service status ticker) -join ' ' }

Oku add (Join-Path $fixtures 'ticker.toml') --yes --service
Start-Sleep -Seconds 3
Check 'an enabled service is running' { (ServiceStatus) -match 'running' -and (ServiceStatus) -match 'starts at login' }
Check 'its task has a logon trigger and runs with the least rights' {
    $task = Get-ScheduledTask -TaskName 'oku-ticker'
    ($task.Triggers.Count -eq 1) -and ($task.Principal.RunLevel -eq 'Limited')
}
$logs = (& $oku service logs ticker) -join ' '
Check 'the program got its arguments and environment, and its output is in the log' {
    $logs -match 'started with PORT=8080 and --data'
}

Oku service stop ticker
Start-Sleep -Seconds 2
Check 'stop stops the task and the program' {
    (ServiceStatus) -match 'stopped' -and -not (Get-Process ticker -ErrorAction SilentlyContinue)
}
Oku service start ticker
Start-Sleep -Seconds 3
Check 'start starts it again' { (ServiceStatus) -match 'running' }

Oku remove ticker
Check 'remove deletes the task' { -not (Get-ScheduledTask -TaskName 'oku-ticker' -ErrorAction SilentlyContinue) }
Start-Sleep -Seconds 2
Check 'remove stops the program' { -not (Get-Process ticker -ErrorAction SilentlyContinue) }

# System scope: an app, a font and a service for the whole machine. The runner is
# already an administrator, so oku runs the privileged step directly.
Set-Content (Join-Path $fixtures 'sysdemo.toml') @"
[package]
name = "sysdemo"
[version]
value = "1.0.0"
[build]
needs = ["go"]
[[build.step]]
run = "Copy-Item '$tickerGo' main.go; Set-Content go.mod 'module sysdemo'; go build -o sysdemo.exe .; Copy-Item (Join-Path `$env:SystemRoot 'Fonts/arial.ttf') OkuSystem.ttf"
shell = "pwsh"
[[build.step]]
install = { bin = ["sysdemo.exe"], font = ["OkuSystem.ttf"] }
[[app]]
name = "Oku System Demo"
exec = "bin/sysdemo.exe"
[[service]]
name = "sysdemo"
command = "bin/sysdemo.exe"
args = ["--system"]
env = { PORT = "9090" }
"@

$sysShortcut = Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs\oku-oku-system-demo.lnk'
$sysFont = Join-Path $env:SystemRoot 'Fonts\OkuSystem.ttf'
$sysFontsKey = 'HKLM:\Software\Microsoft\Windows NT\CurrentVersion\Fonts'

$asked = ('y' | & $oku add (Join-Path $fixtures 'sysdemo.toml') --yes --system --service 2>&1) -join "`n"
if ($LASTEXITCODE -ne 0) { throw "oku add --system failed: $asked" }
Check 'add --system names what it writes before it writes it' {
    ($asked -match 'administrator rights') -and ($asked -match [regex]::Escape($sysFont))
}
Start-Sleep -Seconds 3
Check 'the shortcut and the font are in the machine-wide places' {
    (Test-Path $sysShortcut) -and (Test-Path $sysFont) -and
    ((Get-ItemProperty $sysFontsKey).'oku OkuSystem.ttf' -eq $sysFont)
}
Check 'the service task runs as SYSTEM from boot' {
    $task = Get-ScheduledTask -TaskName 'oku-sysdemo'
    ($task.Principal.UserId -eq 'SYSTEM') -and ($task.Triggers[0].CimClass.CimClassName -eq 'MSFT_TaskBootTrigger')
}
Check 'the program runs as SYSTEM' {
    (Get-Process sysdemo -IncludeUserName).UserName -match 'SYSTEM'
}
Check 'its log is under ProgramData' {
    (Get-Content (Join-Path $env:ProgramData 'oku\logs\sysdemo.log')) -match 'PORT=9090 and --system'
}

$left = (& $oku remove sysdemo 2>&1) -join "`n"
Check 'remove without --system leaves system scope alone and says so' {
    ($left -match 'oku sync --system') -and (Test-Path $sysFont) -and
    (Get-ScheduledTask -TaskName 'oku-sysdemo' -ErrorAction SilentlyContinue)
}

$removal = ('y' | & $oku sync --system 2>&1) -join "`n"
Write-Host $removal
Start-Sleep -Seconds 3
Check 'sync --system removes the shortcut' { -not (Test-Path $sysShortcut) }
Check 'sync --system removes the font' { -not (Test-Path $sysFont) }
Check 'sync --system removes the font from the registry' {
    -not ((Get-ItemProperty $sysFontsKey).PSObject.Properties.Name -contains 'oku OkuSystem.ttf')
}
Check 'sync --system removes the task' { -not (Get-ScheduledTask -TaskName 'oku-sysdemo' -ErrorAction SilentlyContinue) }
Check 'sync --system stops the program' { -not (Get-Process sysdemo -ErrorAction SilentlyContinue) }

# The shared store root.
$shared = Join-Path $env:ProgramData 'oku'
Oku setup --system --yes
Check 'setup --system creates the shared root and lets the user write to it' {
    (Test-Path $shared) -and ((Get-Content "$env:XDG_CONFIG_HOME\oku\config.toml") -match 'store_root')
}
Oku sync
Check 'sync installs into the shared root' { Get-ChildItem (Join-Path $shared 'store') }

# An .msi download, which oku unpacks with "msiexec /a" and never installs.
Set-Content (Join-Path $fixtures 'gh.toml') @'
[package]
name = "gh"
[version]
value = "2.101.0"
[[artifact]]
match = { os = "windows", arch = "amd64" }
url = "https://github.com/cli/cli/releases/download/v{{version}}/gh_{{version}}_windows_amd64.msi"
sha256 = "9ba92256a431d254706844ee1991f6f4a9559a3c3646ff7ae7fe23724bfaef83"
bin = ["Program Files/GitHub CLI/gh.exe"]
'@

Oku add (Join-Path $fixtures 'gh.toml')
$ghVersion = & "$bin\gh.exe" --version
Check 'a program from an msi runs through its shim' { ($ghVersion -join ' ') -match 'gh version 2\.101\.0' }
# The runner has its own gh in Program Files, so that path proves nothing here.
Check 'the copy of the msi that msiexec leaves behind is gone' {
    -not (Get-ChildItem "$env:XDG_DATA_HOME\oku\store\gh-*\pkg\*.msi")
}

# A .7z download, made by 7-Zip on Windows, so it carries no unix modes.
Set-Content (Join-Path $fixtures 'sevenzip.toml') @'
[package]
name = "sevenzip-extra"
[version]
value = "26.03"
[[artifact]]
match = { os = "windows", arch = "amd64" }
url = "https://github.com/ip7z/7zip/releases/download/{{version}}/7z2603-extra.7z"
sha256 = "191894e6acb3647ffb69ce630479ff318523b2e2b9890aa7f05c1127c2e59b8f"
bin = ["x64/7za.exe"]
'@

Oku add (Join-Path $fixtures 'sevenzip.toml')
$banner = (& "$bin\7za.exe") -join ' '
Check 'a program from a 7z archive runs through its shim' { $banner -match '7-Zip \(a\) 26\.03' }

# Uninstall, which has to delete the running oku.exe and the junctions.
Set-Location $env:RUNNER_TEMP
Oku self uninstall --yes --system
Check 'uninstall --system removes the shared root' { -not (Test-Path (Join-Path $env:ProgramData 'oku')) }
Check 'oku.exe is no longer at its path' { -not (Test-Path $oku) }
Check 'data, cache and config are gone' {
    -not (Test-Path "$env:XDG_DATA_HOME\oku") -and -not (Test-Path "$env:XDG_CACHE_HOME\oku") -and
    -not (Test-Path "$env:XDG_CONFIG_HOME\oku")
}
Check 'uninstall took the shortcut, the font and its registry value away' {
    -not (Test-Path $shortcut) -and -not (Test-Path $font) -and
    -not ((Get-ItemProperty $fontsKey).PSObject.Properties.Name -contains 'oku OkuLive.ttf')
}
Check 'the project list and lock are untouched' {
    (Test-Path "$project\oku.toml") -and (Test-Path "$project\oku.lock")
}
Start-Sleep -Seconds 8
Check 'the file that was moved aside is deleted once oku has exited' {
    -not (Test-Path "$oku.uninstalled")
}

# The install script, against a release that a local web server offers.
$served = Join-Path $env:RUNNER_TEMP 'oku-release'
$download = Join-Path $served 'latest\download'
New-Item -ItemType Directory -Force $download | Out-Null
Push-Location $repoRoot
go build -ldflags '-X main.version=0.0.1' -o (Join-Path $download 'oku-windows-amd64.exe') ./cmd/oku
Pop-Location
if ($LASTEXITCODE -ne 0) { throw 'go build for the install test failed' }
$digest = (Get-FileHash (Join-Path $download 'oku-windows-amd64.exe') -Algorithm SHA256).Hash.ToLower()
Set-Content (Join-Path $download 'checksums.txt') "$digest  oku-windows-amd64.exe"

$server = Start-Process python -ArgumentList '-m', 'http.server', '18767', '--directory', $served -PassThru -WindowStyle Hidden

# The server needs a moment, and how long differs from run to run.
$up = $false
foreach ($attempt in 1..30) {
    try {
        Invoke-WebRequest 'http://127.0.0.1:18767/latest/download/checksums.txt' -UseBasicParsing | Out-Null
        $up = $true
        break
    }
    catch { Start-Sleep -Seconds 1 }
}
if (-not $up) { throw 'the local release server did not start within 30 seconds' }
try {
    $env:OKU_RELEASE_URL = 'http://127.0.0.1:18767'
    $env:OKU_INSTALL_DIR = Join-Path $env:RUNNER_TEMP 'oku-installed'
    $said = (& (Join-Path $repoRoot 'install.ps1') 6>&1) -join "`n"
    $installedVersion = & (Join-Path $env:OKU_INSTALL_DIR 'oku.exe') --version
    Check 'install.ps1 puts a working oku.exe in place and prints the hook line' {
        ($installedVersion -match '0\.0\.1') -and ($said -match 'hook pwsh') -and ($said -match 'PROFILE')
    }

    Add-Content (Join-Path $download 'oku-windows-amd64.exe') 'x'
    $env:OKU_INSTALL_DIR = Join-Path $env:RUNNER_TEMP 'oku-tampered'
    $refused = $false
    try { & (Join-Path $repoRoot 'install.ps1') | Out-Null } catch { $refused = $_.Exception.Message -match 'sha256' }
    Check 'install.ps1 refuses a binary that does not match checksums.txt' {
        $refused -and -not (Test-Path (Join-Path $env:OKU_INSTALL_DIR 'oku.exe'))
    }
}
finally {
    Stop-Process -Id $server.Id -Force
    Remove-Item Env:OKU_RELEASE_URL, Env:OKU_INSTALL_DIR
    Remove-Item -Recurse -Force $served, (Join-Path $env:RUNNER_TEMP 'oku-installed') -ErrorAction SilentlyContinue
}

# Hosts other than GitHub, and a URL that is the download itself. The self
# uninstall check above deleted oku.exe, so this part builds it again.
go build -C $repoRoot -o $oku ./cmd/oku
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }

Oku add gitlab:gitlab-org/cli --bin glab
$glab = & "$bin\glab.exe" --version
Check 'a gitlab: project installs from a zip with its program in a directory' { $glab -match '^glab \d' }

Oku add gitea:gitea.com/gitea/tea
$tea = (& "$bin\tea.exe" --version) -join "`n"
Check 'a gitea: repo installs from a single .exe' { $tea -match '\d+\.\d+' }

Oku add https://github.com/sharkdp/hyperfine/releases/download/v1.19.0/hyperfine-v1.19.0-x86_64-pc-windows-msvc.zip
$hyperfine = & "$bin\hyperfine.exe" --version
Check 'a URL of the download installs, with its version from the file name' {
    $hyperfine -match '^hyperfine 1\.19\.0'
}

Remove-Item -Recurse -Force $root
Write-Host 'live test passed'
