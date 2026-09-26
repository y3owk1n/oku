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
Check 'bin is a junction to the links that generations share' {
    ((Get-Item $bin).LinkType -eq 'Junction') -and ((Get-Item $bin).Target -match '\\trees\\')
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
$profileDir = Join-Path $env:XDG_DATA_HOME 'oku\profiles\global'
Check 'a switch of generation leaves no spare junction' {
    -not (Test-Path "$profileDir\current.tmp") -and -not (Test-Path "$profileDir\current.old")
}

# This script holds the lock the way another oku would, on the byte oku locks.
$busy = [IO.File]::Open((Join-Path $env:XDG_DATA_HOME 'oku\busy'), 'OpenOrCreate', 'ReadWrite', 'ReadWrite')
$busy.Lock(1GB, 1)
$waitLog = Join-Path $root 'wait.log'
$waiter = Start-Process $oku -ArgumentList 'gc', '--dry-run' -NoNewWindow -PassThru -RedirectStandardError $waitLog
$null = $waiter.Handle # without it ExitCode stays empty
Start-Sleep -Seconds 2
Check 'a second oku waits while another one holds the lock' { -not $waiter.HasExited }
$busy.Unlock(1GB, 1)
$busy.Close()
$waiter.WaitForExit()
Check 'it says what it waits for, and runs once the lock is free' {
    ($waiter.ExitCode -eq 0) -and ((Get-Content $waitLog -Raw) -match 'waiting for')
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
Set-Content (Join-Path $project 'oku.toml') @'
[packages]
fd = "github:sharkdp/fd"

[[env.file]]
path = ".env"

[env]
STAGE = "dev"
PATH = { prepend = ["scripts"] }
'@
Set-Content (Join-Path $project '.env') "REGION=`"eu west`"`n"
$env:PATH = "$root;$env:PATH"
$env:STAGE = 'mine'

Set-Location $project
Oku sync
Oku allow
# exec finds fd.exe in the project's bin, with no hook in this shell yet.
$fdVersion = & $oku exec fd --version
Check 'exec runs a program of the project by its name without .exe' { $fdVersion -match '^fd \d' }
Set-Location $root

# The profile's bin is on PATH already, behind the system's directories, as in
# a shell that another one started. The hook moves it to the front, once.
$env:PATH = "$env:PATH$([IO.Path]::PathSeparator)$bin"
Invoke-Expression ((& oku hook pwsh) -join [Environment]::NewLine)
Invoke-Expression ((& oku hook pwsh) -join [Environment]::NewLine)
Check 'the hook wrapped the prompt' { Test-Path Function:\_oku_prompt }
$entries = $env:PATH -split [IO.Path]::PathSeparator
Check 'the hook puts the profile bin first on PATH, once' {
    ($entries[0] -eq $bin) -and (@($entries | Where-Object { $_ -eq $bin }).Count -eq 1)
}

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
Check 'inside the project its [env] applies' {
    ($env:STAGE -eq 'dev') -and ($env:REGION -eq 'eu west') -and
    (($env:PATH -split ';')[0] -eq (Join-Path $project 'scripts'))
}

Set-Location $root
prompt | Out-Null
Check 'leaving the project takes fd away again' { -not (Get-Command fd -ErrorAction SilentlyContinue) }
Check 'leaving the project restores STAGE and drops its PATH entry' {
    ($env:STAGE -eq 'mine') -and (-not $env:REGION) -and
    (($env:PATH -split ';') -notcontains (Join-Path $project 'scripts'))
}

# A second fd comes after the profile on PATH, as a global npm package would.
# The shell runs the profile's fd first. After oku removes fd, the next prompt
# makes fd run the second one. After oku adds it back, the profile's fd runs again.
$other = Join-Path $root 'other'
New-Item -ItemType Directory -Force $other | Out-Null
Set-Content (Join-Path $other 'fd.cmd') '@echo other fd'
$withoutOther = $env:PATH
$env:PATH = "$env:PATH;$other"
& fd --version | Out-Null
Oku remove fd
prompt | Out-Null
$fdVersion = & fd --version
Check 'after remove the next prompt runs the fd behind the profile' { $fdVersion -eq 'other fd' }
Oku add github:sharkdp/fd
prompt | Out-Null
$fdVersion = & fd --version
Check 'after add the next prompt runs the fd of the profile again' { $fdVersion -match '^fd \d' }
$env:PATH = $withoutOther

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
install = { bin = ["hello.exe"], share = ["where.txt"], font = ["OkuLive.ttf"], app = [{ path = "hello.exe", name = "Oku Hello" }] }
"@

Set-Location $root
Oku add (Join-Path $fixtures 'hello.toml') --yes --verbose
$shimSpec = Get-Content "$bin\hello.shim"
Check 'the shim leaves the build dep off PATH' {
    ($shimSpec -join "`n") -notmatch 'dir = .*greet-1\.0\.0-'
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
# NTFS cannot clone a file, so the font is a plain copy of the one in the store:
# the same bytes, and no link to it (B379). Windows keeps a registered font open,
# so nothing here writes to it.
Check 'the font is a copy of its own, not a link into the store' {
    $stored = Get-ChildItem -Recurse -Filter OkuLive.ttf (Join-Path $env:XDG_DATA_HOME 'oku\store') |
        Select-Object -First 1
    (-not (Get-Item $font).LinkType) -and
    ((Get-FileHash $font).Hash -eq (Get-FileHash $stored.FullName).Hash)
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
install = { bin = ["sysdemo.exe"], font = ["OkuSystem.ttf"], app = [{ path = "sysdemo.exe", name = "Oku System Demo" }] }
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

# Two .msi downloads in one sync unpack at once, and Windows Installer runs one
# installation at a time, so oku runs msiexec for one after the other.
$msiProject = Join-Path $root 'msi-project'
New-Item -ItemType Directory -Force $msiProject | Out-Null
$msiRefs = foreach ($name in 'msi-a', 'msi-b') {
    Set-Content (Join-Path $fixtures "$name.toml") @"
[package]
name = "$name"
[version]
value = "2.101.0"
[[artifact]]
match = { os = "windows", arch = "amd64" }
url = "https://github.com/cli/cli/releases/download/v{{version}}/gh_{{version}}_windows_amd64.msi"
sha256 = "9ba92256a431d254706844ee1991f6f4a9559a3c3646ff7ae7fe23724bfaef83"
data = true
"@
    "$name = '$((Join-Path $fixtures "$name.toml") -replace '\\', '/')'"
}
Set-Content (Join-Path $msiProject 'oku.toml') ("[packages]`n" + ($msiRefs -join "`n") + "`n")
Set-Location $msiProject
Oku sync
Set-Location $root
Check 'two .msi downloads unpack in one sync' {
    (Get-Content (Join-Path $msiProject 'oku.lock') -Raw) -match "msi-a[\s\S]*msi-b"
}

# A temporary directory that an ended oku process left, which gc removes.
$ended = Start-Process cmd -ArgumentList '/c', 'exit' -PassThru -Wait -WindowStyle Hidden
$leftover = Join-Path $env:TEMP "oku-build-$($ended.Id)-1"
New-Item -ItemType Directory -Force $leftover | Out-Null
Set-Content (Join-Path $leftover 'half') 'unpacked'
Oku gc
Check 'gc removes a temporary directory that an ended oku process left' { -not (Test-Path $leftover) }

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

# Two store paths with the same files. The data file becomes one hard link that
# neither can change. A program keeps its own copy, since Windows refuses to
# delete any link to a running program, and gc must still delete the other path.
foreach ($name in 'share-a', 'share-b') {
    Set-Content (Join-Path $fixtures "$name.toml") @"
[package]
name = "$name"
[version]
value = "1.0.0"
[build]
[[build.step]]
run = "New-Item -ItemType Directory -Force '{{prefix}}/bin' | Out-Null; Copy-Item `$env:SystemRoot/System32/PING.EXE '{{prefix}}/bin/$name.exe'; [IO.File]::WriteAllBytes('{{prefix}}/data.bin', [byte[]]::new(20000))"
shell = "pwsh"
"@
    Oku add (Join-Path $fixtures "$name.toml") --yes
}

# setup --system above moved the store to the shared root.
$store = Join-Path $shared 'store'
$dataB = (Get-Item "$store\share-b-*\data.bin").FullName
$dataLinks = (fsutil hardlink list $dataB) -join "`n"
Check 'two store paths share one data file' { $dataLinks -match 'share-a-' }
Check 'the shared data file is read-only' { (Get-Item $dataB).IsReadOnly }

$programLinks = (fsutil hardlink list (Get-Item "$store\share-b-*\bin\share-b.exe").FullName) -join "`n"
Check 'a program is not shared' { $programLinks -notmatch 'share-a-' }

# Every shim is a hard link of one file, so the old generations hold links to
# the shim that runs. gc moves those aside and deletes them once it has ended.
$pinger = Start-Process "$bin\share-b.exe" -ArgumentList '-n', '30', '127.0.0.1' -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 1
Oku remove share-a
Oku gc --keep 1
Check 'gc deletes a store path while a program of the other one runs' { -not (Test-Path "$store\share-a-*") }
Check 'gc deletes the old generations while a shim runs' {
    @(Get-ChildItem $profileDir -Directory -Filter 'gen-*').Count -eq 1
}
Check 'the shim keeps running' { -not $pinger.HasExited }
# Stopping the shim does not stop the program it started, so the test stops both.
Stop-Process $pinger -ErrorAction SilentlyContinue
Get-Process share-b -ErrorAction SilentlyContinue | Stop-Process
$pinger.WaitForExit()
Wait-Process share-b -ErrorAction SilentlyContinue

Check 'the kept data file is whole and still read-only' {
    ((Get-Item $dataB).Length -eq 20000) -and (Get-Item $dataB).IsReadOnly
}

$zeros = Join-Path $root 'zeros.bin'
[IO.File]::WriteAllBytes($zeros, [byte[]]::new(20000))
$key = (Get-FileHash $zeros -Algorithm SHA256).Hash.ToLower()
Oku remove share-b
Oku gc --keep 1
Check 'gc drops the shared file once no store path holds it' { -not (Test-Path "$store\.links\$key") }
Check 'gc deletes what it moved aside once the program has ended' {
    @((Join-Path $env:XDG_DATA_HOME 'oku\trash'), (Join-Path $shared 'trash')) | ForEach-Object {
        -not (Test-Path $_) -or @(Get-ChildItem $_).Count -eq 0
    } | Where-Object { -not $_ } | Measure-Object | ForEach-Object { $_.Count -eq 0 }
}

# When a shim ends, Windows ends the program it started, as stopping a program
# does on macOS and Linux.
Set-Content (Join-Path $fixtures 'linger.toml') @"
[package]
name = "linger"
[version]
value = "1.0.0"
[build]
[[build.step]]
run = "New-Item -ItemType Directory -Force '{{prefix}}/bin' | Out-Null; Copy-Item `$env:SystemRoot/System32/PING.EXE '{{prefix}}/bin/linger.exe'; Copy-Item `$env:SystemRoot/System32/cmd.exe '{{prefix}}/bin/launch.exe'"
shell = "pwsh"
"@
Oku add (Join-Path $fixtures 'linger.toml') --yes
$lingerShim = Start-Process "$bin\linger.exe" -ArgumentList '-n', '120', '127.0.0.1' -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 2
Check 'a shim and the program it started run' { @(Get-Process linger -ErrorAction SilentlyContinue).Count -eq 2 }
Stop-Process $lingerShim
Start-Sleep -Seconds 2
Check 'stopping the shim stops the program it started' { -not (Get-Process linger -ErrorAction SilentlyContinue) }

# A process that the program starts keeps running, as an editor that a launcher
# opens does. launch is a copy of cmd, which starts ping and waits.
$launchShim = Start-Process "$bin\launch.exe" -ArgumentList '/c', 'ping -n 120 127.0.0.1' -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 2
Stop-Process $launchShim
Start-Sleep -Seconds 2
Check 'stopping the shim leaves what its program started' {
    -not (Get-Process launch -ErrorAction SilentlyContinue) -and (Get-Process PING -ErrorAction SilentlyContinue)
}
Get-Process PING -ErrorAction SilentlyContinue | Stop-Process

# A project whose folder is gone takes its profile with it on the next gc.
$goneProject = Join-Path $root 'gone-project'
New-Item -ItemType Directory -Force $goneProject | Out-Null
Set-Content (Join-Path $goneProject 'oku.toml') ''
Push-Location $goneProject
Oku add (Join-Path $fixtures 'linger.toml') --yes
Pop-Location
$projectProfiles = { @(Get-ChildItem (Join-Path $env:XDG_DATA_HOME 'oku\profiles') -Directory -Filter 'project-*').Count }
$profilesBefore = & $projectProfiles
Remove-Item -Recurse -Force $goneProject
$gcOut = (Oku gc) -join "`n"
Check 'gc removes the profile of a project whose folder is gone' {
    ((& $projectProfiles) -eq $profilesBefore - 1) -and ($gcOut -match 'its folder is gone')
}

# Uninstall, which has to delete the running oku.exe and the junctions, while a
# program that oku installed runs from the shared store through its shim.
$null = Start-Process "$bin\linger.exe" -ArgumentList '-n', '120', '127.0.0.1' -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 2
Set-Location $env:RUNNER_TEMP
$uninstalled = (Oku self uninstall --yes --system) -join "`n"
Check 'uninstall says that a program it installed still runs' { $uninstalled -match 'still runs' }
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
Get-Process linger -ErrorAction SilentlyContinue | Stop-Process
# The deleting cmd tries every ten seconds.
for ($i = 0; $i -lt 30 -and ((Test-Path "$env:XDG_DATA_HOME\oku-uninstalled") -or (Test-Path "$env:ProgramData\oku-uninstalled")); $i++) {
    Start-Sleep -Seconds 1
}
Check 'the files of the program are deleted once it has ended' {
    -not (Test-Path "$env:XDG_DATA_HOME\oku-uninstalled") -and -not (Test-Path "$env:ProgramData\oku-uninstalled")
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
        ($installedVersion -match '0\.0\.1') -and ($said -match 'hook pwsh') -and ($said -match 'PROFILE') -and
        ($said -match 'installed oku version 0\.0\.1') -and ($said -match 'oku doctor')
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

# gitea.com at times answers the runners with a 504 or a 409. That answer is
# the server's, so the check is skipped then, and any other failure fails it.
$added = (& $oku add gitea:gitea.com/gitea/tea 2>&1) -join "`n"
if ($LASTEXITCODE -eq 0) {
    $tea = (& "$bin\tea.exe" --version) -join "`n"
    Check 'a gitea: repo installs from a single .exe' { $tea -match '\d+\.\d+' }
} elseif ($added -match 'returned (5\d\d|409) ') {
    Write-Host "skipped: a gitea: repo installs, because gitea.com failed:`n$added"
} else {
    throw "oku add gitea:gitea.com/gitea/tea exited with $LASTEXITCODE`n$added"
}

# A git-tags source on github.com: oku lists the tags through the API and never
# runs git for it (B381).
Set-Content (Join-Path $fixtures 'rgtags.toml') @'
[package]
name = "rgtags"
[version]
from = "git-tags"
repo = "https://github.com/BurntSushi/ripgrep"
[[artifact]]
match = { os = "windows", arch = "amd64" }
url = "https://github.com/BurntSushi/ripgrep/releases/download/{{version}}/ripgrep-{{version}}-x86_64-pc-windows-msvc.zip"
strip = 1
bin = ["rg.exe"]
'@
# The version is named, so the check holds when upstream tags a new one, and the
# tag list still has to hold it.
Oku add ((Join-Path $fixtures 'rgtags.toml') + '@15.2.0') --accept-unknown-age
$tagged = & "$bin\rg.exe" --version
Check 'a git-tags version installs from the tags the forge API lists' {
    $tagged -match '^ripgrep 15\.2\.0'
}

Oku add https://github.com/sharkdp/hyperfine/releases/download/v1.19.0/hyperfine-v1.19.0-x86_64-pc-windows-msvc.zip
$hyperfine = & "$bin\hyperfine.exe" --version
Check 'a URL of the download installs, with its version from the file name' {
    $hyperfine -match '^hyperfine 1\.19\.0'
}

Oku add https://github.com/jqlang/jq/releases/download/jq-1.8.1/jq-windows-amd64.exe
$jq = & "$bin\jq.exe" --version
Check 'a URL of a single .exe installs as a program' { $jq -match '^jq-1\.8\.1' }

$page = (& $oku add https://github.com/BurntSushi/ripgrep 2>&1) -join "`n"
Check 'a URL of a repo page is refused and names the github: ref' {
    ($LASTEXITCODE -ne 0) -and ($page -match 'web page') -and ($page -match 'github:BurntSushi/ripgrep')
}

# A bin table: a program that runs a dep with arguments in front of the user's.
# Node stands in for any interpreter, and "-p" prints what it evaluates.
New-Item -ItemType Directory -Force $fixtures | Out-Null
Set-Content (Join-Path $fixtures 'node.toml') @'
[package]
name = "node"
[version]
value = "22.20.0"
[[artifact]]
match = { os = "windows", arch = "amd64" }
url = "https://nodejs.org/dist/v{{version}}/node-v{{version}}-win-x64.zip"
sha256_url = "https://nodejs.org/dist/v{{version}}/SHASUMS256.txt"
strip = 1
bin = ["node.exe"]
'@
Set-Content (Join-Path $fixtures 'evaluate.toml') @'
[package]
name = "evaluate"
[version]
value = "0.3.61"
[runtime]
deps = ["./node.toml"]
[[artifact]]
url = "https://registry.npmjs.org/@actions/languageserver/-/languageserver-{{version}}.tgz"
sha256 = "d152725064c64f862da5158cd630d4c67973edfb58bdabaa44054ffef03b9d03"
strip = 1
bin = [{ name = "evaluate", run = "{{dep.node.prefix}}/bin/node.exe", args = ["-p"] }]
'@

Oku add (Join-Path $fixtures 'evaluate.toml')
$evaluated = & "$bin\evaluate.exe" '6*7'
Check 'a bin table runs the dep with its arguments before the user ones' { $evaluated -eq '42' }
Check 'the dep of a bin table stays out of the profile' { -not (Test-Path "$bin\node.exe") }

# An npm package. Windows cannot run a script through PATH, so oku asks for a
# node first, and then runs the package's programs through it.
$refused = (& $oku add npm:prettier 2>&1) -join "`n"
Check 'an npm package without runtimes.node is refused and the error names the key and the example' {
    ($LASTEXITCODE -ne 0) -and ($refused -match 'runtimes\.node') -and ($refused -match 'examples/runtimes/node\.toml')
}

$configDir = Join-Path $env:XDG_CONFIG_HOME 'oku'
New-Item -ItemType Directory -Force $configDir | Out-Null
Set-Content (Join-Path $configDir 'config.toml') "[runtimes]`nnode = '$(Join-Path $fixtures 'node.toml')'"

Oku add npm:prettier
$prettier = & "$bin\prettier.exe" --version
Check 'an npm package runs through the configured node' { $prettier -match '^\d+\.\d+' }

# An npm package that lists dependencies. npm installs them with the build, and
# on Windows it runs as npm's own script beside the node.exe of the node package.
Oku add --yes npm:cowsay
$moo = (& "$bin\cowsay.exe" moo) -join "`n"
Check 'an npm package with dependencies installs and runs on Windows' { $moo -match 'moo' }

# [files]. A normal Windows user cannot create a symlink, so a linked directory
# is a junction, and a linked file or a text is a copy.
$listPath = Join-Path $configDir 'oku.toml'
$withoutFiles = Get-Content $listPath -Raw
$sources = Join-Path $configDir 'files'
New-Item -ItemType Directory -Force (Join-Path $sources 'nvim') | Out-Null
Set-Content (Join-Path $sources 'nvim\init.lua') 'first'
Set-Content (Join-Path $sources 'gitconfig') 'linked file'
[IO.File]::WriteAllText((Join-Path $sources 'greeting.tmpl'), "say {{ greeting }}`r`n\{{kept}}")

$nvim = Join-Path $env:XDG_CONFIG_HOME 'nvim'
$gitconfig = Join-Path $env:XDG_CONFIG_HOME 'git\config'
$note = Join-Path $env:LOCALAPPDATA 'oku-live-files\note.txt'

Add-Content $listPath @'

[vars]
greeting = "hi"

[files]
"{{config}}/greeting.txt" = { render = "./files/greeting.tmpl" }
"{{config}}/nvim" = { link = "./files/nvim" }
"{{config}}/git/config" = { link = "./files/gitconfig" }
"{{localappdata}}/oku-live-files/note.txt" = { text = "one", when = { os = "windows" } }
'@

Oku sync
Set-Content (Join-Path $sources 'nvim\init.lua') 'second'
Check 'a linked directory is a junction, and an edit of its source shows with no sync' {
    ((Get-Item $nvim).LinkType -eq 'Junction') -and ((Get-Content "$nvim\init.lua") -eq 'second')
}
Check 'a linked file and a text are copies with the right bytes' {
    ((Get-Content $gitconfig) -eq 'linked file') -and ((Get-Content $note -Raw) -eq 'one') -and
    (-not (Get-Item $note).LinkType)
}

Check 'a template renders to the same bytes as on the other systems' {
    [IO.File]::ReadAllText((Join-Path $env:XDG_CONFIG_HOME 'greeting.txt')) -ceq "say hi`r`n{{kept}}"
}

Set-ItemProperty $note IsReadOnly $false
Set-Content $note 'edited by hand' -NoNewline
$refused = (& $oku sync 2>&1) -join "`n"
Check 'B141: a copy that was edited by hand stops the sync, is named, and keeps the edit' {
    ($LASTEXITCODE -ne 0) -and ($refused -match 'note\.txt') -and
    ((Get-Content $note -Raw) -eq 'edited by hand')
}

Remove-Item -Force $note
Oku sync
Check 'a copy that was deleted is written again' { (Get-Content $note -Raw) -eq 'one' }

(Get-Content $listPath -Raw).Replace('text = "one"', 'text = "two"') | Set-Content $listPath -NoNewline
Oku sync
Check 'a changed text reaches the copy' { (Get-Content $note -Raw) -eq 'two' }

Oku rollback
Check 'rollback brings back the bytes of the generation before' { (Get-Content $note -Raw) -eq 'one' }

Set-Content $listPath $withoutFiles -NoNewline
Oku sync
Check 'the targets are gone once [files] is, and the source of the junction is kept' {
    (-not (Test-Path $nvim)) -and (-not (Test-Path $gitconfig)) -and (-not (Test-Path $note)) -and
    ((Get-Content (Join-Path $sources 'nvim\init.lua')) -eq 'second')
}
Remove-Item -Recurse -Force (Join-Path $env:LOCALAPPDATA 'oku-live-files') -ErrorAction SilentlyContinue

# [registry]. Two values exist before oku, so that it has something to put back.
$regKey = 'HKCU:\Software\oku-live-test'
New-Item -Force $regKey | Out-Null
Set-ItemProperty $regKey -Name Delay -Value 'before oku'
Set-ItemProperty $regKey -Name Count -Value 7 -Type DWord

Add-Content $listPath @'

[registry.'HKCU\Software\oku-live-test']
Delay = "0"
Count = 9
Enabled = true
Size = 48
Names = ["one", "two"]

[defaults."com.apple.dock"]
tilesize = 48
'@

Oku sync
$props = Get-ItemProperty $regKey
$kinds = Get-Item $regKey
Check 'registry values arrive with their types, and the table of macOS is skipped' {
    ($props.Delay -eq '0') -and ($props.Count -eq 9) -and ($props.Enabled -eq 1) -and ($props.Size -eq 48) -and
    (($props.Names -join ',') -eq 'one,two') -and ($kinds.GetValueKind('Delay') -eq 'String') -and
    ($kinds.GetValueKind('Size') -eq 'DWord') -and ($kinds.GetValueKind('Names') -eq 'MultiString')
}

Set-Content $listPath $withoutFiles -NoNewline
Oku sync
$props = Get-ItemProperty $regKey
Check 'a value that leaves the list gets back what it held before oku, and a new one is deleted' {
    ($props.Delay -eq 'before oku') -and ($props.Count -eq 7) -and
    ((Get-Item $regKey).GetValueKind('Count') -eq 'DWord') -and
    ($null -eq $props.Enabled) -and ($null -eq $props.Size) -and ($null -eq $props.Names)
}
Remove-Item -Recurse -Force $regKey

# Secrets. oku installs age, which then makes a throwaway key and an encrypted
# file. oku decrypts an age file itself, so the sync needs neither program.
Set-Content (Join-Path $fixtures 'age.toml') @'
[package]
name = "age"
[version]
value = "1.3.2"
[[artifact]]
match = { os = "windows", arch = "amd64" }
url = "https://github.com/FiloSottile/age/releases/download/v{{version}}/age-v{{version}}-windows-amd64.zip"
strip = 1
bin = ["age.exe", "age-keygen.exe"]
'@
Oku add (Join-Path $fixtures 'age.toml')

$keys = Join-Path $env:XDG_CONFIG_HOME 'sops\age\keys.txt'
New-Item -ItemType Directory -Force (Split-Path $keys) | Out-Null
& "$bin\age-keygen.exe" -o $keys 2>$null
$recipient = & "$bin\age-keygen.exe" -y $keys

$plain = Join-Path $root 'plain.txt'
[IO.File]::WriteAllText($plain, 'windows-secret')
& "$bin\age.exe" -r $recipient -o (Join-Path $sources 'key.age') $plain
if ($LASTEXITCODE -ne 0) { throw 'age could not encrypt' }
Remove-Item $plain

$withoutSecret = Get-Content $listPath -Raw
Add-Content $listPath "`n[files]`n`"{{config}}/secret-key`" = { secret = `"./files/key.age`" }`n"
Oku sync

$secretTarget = Join-Path $env:XDG_CONFIG_HOME 'secret-key'
$secretStore = Join-Path $env:XDG_DATA_HOME 'oku\secrets'
Check 'oku decrypts an age secret itself' {
    [IO.File]::ReadAllText($secretTarget) -ceq 'windows-secret'
}

function OnlyTheUser($path) {
    $acl = Get-Acl $path
    $me = [Security.Principal.WindowsIdentity]::GetCurrent().User
    $who = @($acl.Access | ForEach-Object { $_.IdentityReference.Translate([Security.Principal.SecurityIdentifier]) })
    $acl.AreAccessRulesProtected -and ($who.Count -eq 1) -and ($who[0] -eq $me)
}
Check 'only the current user may read the secret, its copy and their directory' {
    (OnlyTheUser $secretTarget) -and (OnlyTheUser $secretStore) -and
    (OnlyTheUser (Get-ChildItem $secretStore)[0].FullName)
}

Set-Content $listPath $withoutSecret -NoNewline
Oku sync
Check 'a secret that leaves the list is deleted with its copy' {
    (-not (Test-Path $secretTarget)) -and (@(Get-ChildItem $secretStore).Count -eq 0)
}

# A Go program from the module proxy, built with the go that config.toml names
# in [runtimes]. The vendor step and go install run through pwsh.
$goToml = Join-Path $fixtures 'go.toml'
Set-Content $goToml @'
[package]
name = "go"
[version]
value = "1.26.4"
[[artifact]]
url = "https://dl.google.com/go/go{{version}}.{{os}}-{{arch}}.zip"
sha256_url = "https://dl.google.com/go/go{{version}}.{{os}}-{{arch}}.zip.sha256"
strip = 1
bin = ["bin/go.exe", "bin/gofmt.exe"]
'@
Add-Content (Join-Path $configDir 'config.toml') "`ngo = '$($goToml -replace '\\', '/')'"
Oku add --yes go:mvdan.cc/gofumpt
$gofumpt = & "$bin\gofumpt.exe" --version
Check 'a go: ref builds on Windows and the program knows its version' { $gofumpt -match '^v\d' }
$gofumptSpec = (Get-Content "$bin\gofumpt.shim") -join "`n"
Check 'the go that built a go: program stays off its PATH' { $gofumptSpec -notmatch '\\go-1\.26\.4-' }

# A crate from crates.io, built with the cargo on PATH, which the runner has from
# rustup. The vendor step and cargo install run through pwsh.
Oku add --yes cargo:hexyl
$hexyl = & "$bin\hexyl.exe" --version
Check 'a cargo: ref builds on Windows' { $hexyl -match '^hexyl \d' }

# Python packages from PyPI with uv, through a python that the list names. oku
# refuses a pypi: ref without one. ruff ships a binary, and httpie console
# scripts, which become shims.
$pythonToml = Join-Path $fixtures 'python.toml'
Set-Content $pythonToml @'
[package]
name = "python"
[version]
value = "3.13.15"
[[artifact]]
url = "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-3.13.15+20260901-x86_64-pc-windows-msvc-install_only.tar.gz"
strip = 1
bin = ["python.exe"]
'@
$refused = (& $oku add pypi:ruff 2>&1) -join "`n"
Check 'a pypi package without runtimes.python is refused and the error names the key and the example' {
    ($LASTEXITCODE -ne 0) -and ($refused -match 'runtimes\.python') -and ($refused -match 'examples/runtimes/python\.toml')
}
Add-Content $listPath "`n[runtimes]`npython = '$($pythonToml -replace '\\', '/')'`n"
Oku add --yes pypi:ruff pypi:httpie
$ruff = & "$bin\ruff.exe" --version
Check 'a pypi: package that ships a binary installs on Windows' { $ruff -match '^ruff \d' }
$http = & "$bin\http.exe" --version
Check 'a console script of a pypi: package runs on Windows through its shim' { $http -match '^\d' }
$httpSpec = (Get-Content "$bin\http.shim") -join "`n"
Check 'a pypi: program finds its python on PATH, and not the uv that installed it' {
    ($httpSpec -match 'dir = .*\\python-3\.13\.15-') -and ($httpSpec -notmatch '\\uv-')
}

# python.exe loads python313.dll from beside the real file in its download, and
# bin in the store holds a link to it, so the shim names the download.
Oku add $pythonToml
$version = & "$bin\python.exe" --version
Check 'a program that loads a DLL beside it in its download runs from its shim' { $version -match '^Python 3\.13' }

# A machine without PowerShell 7 builds registry packages with the Windows
# PowerShell 5.1 that Windows ships. The runner has PowerShell 7, so its
# directory leaves PATH for these two builds.
$pathBefore = $env:PATH
$env:PATH = (($env:PATH -split ';') | Where-Object { $_ -and -not (Test-Path (Join-Path $_ 'pwsh.exe')) }) -join ';'
try {
    Check 'pwsh is off PATH for this case' { -not (Get-Command pwsh -ErrorAction SilentlyContinue) }
    Oku add --yes go:mvdan.cc/sh/v3/cmd/shfmt pypi:cowsay
} finally {
    $env:PATH = $pathBefore
}
$shfmt = & "$bin\shfmt.exe" --version
Check 'a go: ref builds with Windows PowerShell when PowerShell 7 is missing' { $shfmt -match '\d+\.\d+' }
$moo = (& "$bin\cowsay.exe" -t moo) -join "`n"
Check 'a pypi: ref builds with Windows PowerShell when PowerShell 7 is missing' { $moo -match 'moo' }

# In this project the go package is also the runtime of a go: package, so one
# sync downloads the go zip twice at once. Windows refuses to replace the file
# while the other install unpacks it.
$goProject = Join-Path $root 'go-project'
New-Item -ItemType Directory -Force $goProject | Out-Null
Set-Content (Join-Path $goProject 'go.toml') @'
[package]
name = "go"
[version]
value = "1.26.4"
[[artifact]]
url = "https://dl.google.com/go/go{{version}}.{{os}}-{{arch}}.zip"
sha256_url = "https://dl.google.com/go/go{{version}}.{{os}}-{{arch}}.zip.sha256"
strip = 1
bin = ["bin/go.exe", "bin/gofmt.exe"]
'@
Set-Content (Join-Path $goProject 'oku.toml') @'
[runtimes]
go = "./go.toml"
[packages]
go = "./go.toml"
stringer = "go:golang.org/x/tools/cmd/stringer"
'@
Set-Location $goProject
Oku sync --yes
Set-Location $root
Check 'packages that share a download install in one sync' {
    Test-Path (Join-Path $env:XDG_DATA_HOME 'oku\profiles\project-*\current\bin\stringer.exe')
}

# A process that a build started may still hold a directory for a moment.
Set-Location $repoRoot
foreach ($try in 1..30) {
    try { Remove-Item -Recurse -Force $root; break } catch {
        if ($try -eq 30) { Get-Process go* -ErrorAction SilentlyContinue | Format-Table Id, Path; throw }
        Start-Sleep 1
    }
}
Write-Host 'live test passed'
