# Aether 0.6.0 Windows 运行时安装器：powershell -ExecutionPolicy Bypass -File install.ps1
$ErrorActionPreference = "Stop"
$Here = Split-Path -Parent $MyInvocation.MyCommand.Path
$Dest = Join-Path $env:LOCALAPPDATA "Aether"
New-Item -ItemType Directory -Force -Path (Join-Path $Dest "bin") | Out-Null
Copy-Item (Join-Path $Here "aether.exe") (Join-Path $Dest "bin\aether.exe") -Force
foreach ($d in "examples","docs","conformance") {
  if (Test-Path (Join-Path $Here $d)) { Copy-Item (Join-Path $Here $d) $Dest -Recurse -Force }
}
$p = [Environment]::GetEnvironmentVariable("Path","User")
if ($p -notlike "*$Dest\bin*") { [Environment]::SetEnvironmentVariable("Path", "$p;$Dest\bin", "User") }
$env:Path = "$env:Path;$Dest\bin"
& (Join-Path $Dest "bin\aether.exe") version
& (Join-Path $Dest "bin\aether.exe") doctor
$vsix = Get-ChildItem (Join-Path $Here "aether-vscode-*.vsix") -ErrorAction SilentlyContinue | Select-Object -First 1
foreach ($ed in "code","codium","cursor") {
  if ($vsix -and (Get-Command $ed -ErrorAction SilentlyContinue)) {
    & $ed --install-extension $vsix.FullName --force | Out-Null
    Write-Host "已为 $ed 安装 Aether 扩展"
  }
}
Write-Host "运行时安装完成（重新打开终端后 aether 生效）。完整 IDE 请运行 Install-Aether.ps1 或 Aether-Setup-*.exe"
