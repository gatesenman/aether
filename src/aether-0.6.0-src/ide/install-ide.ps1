# Aether IDE per-user installer for Windows: copies the portable build to %LOCALAPPDATA%\Programs\Aether IDE,
# creates Start Menu / Desktop shortcuts and adds bin\ (aether, aether-ide) to the user PATH.
$ErrorActionPreference = "Stop"
$Here = Split-Path -Parent $MyInvocation.MyCommand.Path
$Dest = Join-Path $env:LOCALAPPDATA "Programs\Aether IDE"
New-Item -ItemType Directory -Force -Path $Dest | Out-Null
Copy-Item -Path (Join-Path $Here "*") -Destination $Dest -Recurse -Force
$ws = New-Object -ComObject WScript.Shell
foreach ($lnk in @((Join-Path ([Environment]::GetFolderPath("Programs")) "Aether IDE.lnk"), (Join-Path ([Environment]::GetFolderPath("Desktop")) "Aether IDE.lnk"))) {
  $s = $ws.CreateShortcut($lnk); $s.TargetPath = (Join-Path $Dest "Aether IDE.exe"); $s.IconLocation = (Join-Path $Dest "resources\app\resources\win32\code.ico"); $s.Save()
}
$bin = Join-Path $Dest "bin"
$p = [Environment]::GetEnvironmentVariable("Path", "User")
if ($p -notlike "*$bin*") { [Environment]::SetEnvironmentVariable("Path", "$p;$bin", "User") }
& (Join-Path $bin "aether.exe") version
Write-Host "Aether IDE 已安装到 $Dest（开始菜单与桌面已创建快捷方式）。"
