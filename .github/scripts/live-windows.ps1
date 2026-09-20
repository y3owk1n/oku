$ErrorActionPreference = 'Stop'
$d = Join-Path $env:RUNNER_TEMP 'msi-probe'
New-Item -ItemType Directory -Force $d | Out-Null
Invoke-WebRequest 'https://github.com/cli/cli/releases/download/v2.101.0/gh_2.101.0_windows_amd64.msi' -OutFile "$d\gh.msi"
$p = Start-Process msiexec -ArgumentList '/a', "$d\gh.msi", '/qn', '/norestart', "TARGETDIR=$d\out" -Wait -PassThru
Write-Host "msiexec exit $($p.ExitCode)"
Get-ChildItem -Recurse "$d\out" | Select-Object -First 25 | ForEach-Object { Write-Host "TREE $($_.FullName.Substring($d.Length))" }
(Get-FileHash "$d\gh.msi" -Algorithm SHA256).Hash.ToLower() | ForEach-Object { Write-Host "SHA $_" }
