#Requires -Version 5.1
<#
.SYNOPSIS
  Aether 0.5.1 Windows installer (PowerShell).
.DESCRIPTION
  Installs Aether IDE + aether.exe runtime to %LOCALAPPDATA%\Aether (or -Dest),
  creates Desktop + Start Menu shortcuts, adds bin to user PATH, writes Uninstall-Aether.ps1.
  Also mirrors a copy under GrokBot-Apps\Aether语言\运行时 when that folder exists / -GrokBotApps.
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File Install-Aether.ps1
  powershell -ExecutionPolicy Bypass -File Install-Aether.ps1 -PayloadDir .\aether-ide-win32-x64
  powershell -ExecutionPolicy Bypass -File Install-Aether.ps1 -ZipPath .\Aether-IDE-0.5.1-win32-x64.zip
#>
[CmdletBinding()]
param(
  [string]$Dest = (Join-Path $env:LOCALAPPDATA "Aether"),
  [string]$ZipPath = "",
  [string]$PayloadDir = "",
  [string]$GrokBotApps = "C:\Users\Administrator\GrokBot-Apps\Aether语言",
  [switch]$NoShortcuts,
  [switch]$NoPath
)
$ErrorActionPreference = "Stop"
$Version = "0.5.1"
$Here = Split-Path -Parent $MyInvocation.MyCommand.Path
Write-Host "Aether $Version 安装器" -ForegroundColor Cyan

function Find-Payload {
  if ($PayloadDir -and (Test-Path $PayloadDir)) { return (Resolve-Path $PayloadDir).Path }
  foreach ($c in @(
      (Join-Path $Here "aether-ide-win32-x64"),
      (Join-Path $Here "Aether-IDE"),
      (Join-Path $Here "payload")
    )) {
    if (Test-Path (Join-Path $c "Aether IDE.exe")) { return $c }
    if (Test-Path (Join-Path $c "bin\aether.exe")) { return $c }
  }
  $z = $ZipPath
  if (-not $z) {
    $z = Get-ChildItem $Here -Filter "Aether-IDE-*-win32-x64.zip" -ErrorAction SilentlyContinue |
      Sort-Object LastWriteTime -Descending | Select-Object -First 1 -ExpandProperty FullName
  }
  if (-not $z -or -not (Test-Path $z)) {
    throw "找不到 IDE 包。请把 Aether-IDE-*-win32-x64.zip 或解压后的 aether-ide-win32-x64 放在本脚本同目录。"
  }
  $tmp = Join-Path $env:TEMP ("aether-install-" + [guid]::NewGuid().ToString("n"))
  New-Item -ItemType Directory -Force -Path $tmp | Out-Null
  Write-Host "正在解压 $z ..."
  Expand-Archive -LiteralPath $z -DestinationPath $tmp -Force
  $found = Get-ChildItem $tmp -Recurse -Filter "Aether IDE.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
  if (-not $found) { $found = Get-ChildItem $tmp -Recurse -Filter "aether.exe" | Select-Object -First 1 }
  if (-not $found) { throw "压缩包里没有 Aether IDE.exe / aether.exe" }
  return $found.DirectoryName
  # if exe is in bin\, go up; if IDE.exe at root, that dir
}

$src = Find-Payload
# If we landed in bin\, prefer parent when it has IDE.exe
if ((Split-Path $src -Leaf) -eq "bin") {
  $parent = Split-Path $src -Parent
  if (Test-Path (Join-Path $parent "Aether IDE.exe")) { $src = $parent }
}
Write-Host "安装源: $src"
Write-Host "安装到: $Dest"

New-Item -ItemType Directory -Force -Path $Dest | Out-Null
$IdeDir = Join-Path $Dest "IDE"
$BinDir = Join-Path $Dest "bin"
New-Item -ItemType Directory -Force -Path $IdeDir, $BinDir | Out-Null

# Copy IDE tree
Write-Host "复制 IDE 文件..."
robocopy $src $IdeDir /E /NFL /NDL /NJH /NJS /nc /ns /np | Out-Null
if ($LASTEXITCODE -ge 8) { throw "robocopy failed: $LASTEXITCODE" }

# Ensure aether.exe in Dest\bin
$aeCandidates = @(
  (Join-Path $IdeDir "bin\aether.exe"),
  (Join-Path $src "bin\aether.exe"),
  (Join-Path $Here "aether.exe"),
  (Join-Path $IdeDir "aether.exe")
)
$ae = $aeCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $ae) { throw "找不到 aether.exe" }
Copy-Item $ae (Join-Path $BinDir "aether.exe") -Force

# Thin launcher at Dest\Aether.exe -> starts IDE
$ideExe = Join-Path $IdeDir "Aether IDE.exe"
if (-not (Test-Path $ideExe)) {
  # branded name may vary
  $ideExe = Get-ChildItem $IdeDir -Filter "*.exe" | Where-Object { $_.Name -match "Aether|Code|Codium" } | Select-Object -First 1 -ExpandProperty FullName
}
@"
@echo off
start "" "$ideExe" %*
"@ | Set-Content -Encoding ASCII (Join-Path $Dest "Aether.cmd")
# Also copy a small Go-built Aether.exe launcher if present next to script
if (Test-Path (Join-Path $Here "Aether-Launcher.exe")) {
  Copy-Item (Join-Path $Here "Aether-Launcher.exe") (Join-Path $Dest "Aether.exe") -Force
} elseif (Test-Path $ideExe) {
  # fallback: cmd wrapper only
  Copy-Item (Join-Path $Dest "Aether.cmd") (Join-Path $Dest "启动 Aether IDE.cmd") -Force
}

if (-not $NoShortcuts) {
  $ws = New-Object -ComObject WScript.Shell
  $icon = Join-Path $IdeDir "resources\app\resources\win32\code.ico"
  if (-not (Test-Path $icon)) { $icon = $ideExe }
  foreach ($lnk in @(
      (Join-Path ([Environment]::GetFolderPath("Desktop")) "Aether IDE.lnk"),
      (Join-Path ([Environment]::GetFolderPath("Programs")) "Aether IDE.lnk")
    )) {
    $s = $ws.CreateShortcut($lnk)
    $s.TargetPath = $ideExe
    $s.WorkingDirectory = $IdeDir
    $s.IconLocation = $icon
    $s.Description = "Aether IDE $Version"
    $s.Save()
  }
  Write-Host "已创建桌面与开始菜单快捷方式"
}

if (-not $NoPath) {
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if ($userPath -notlike "*$BinDir*") {
    [Environment]::SetEnvironmentVariable("Path", ($userPath.TrimEnd(';') + ";" + $BinDir), "User")
    $env:Path = $env:Path.TrimEnd(';') + ";" + $BinDir
    Write-Host "已加入用户 PATH: $BinDir"
  }
}

# Uninstall script
$uninst = @"
# Aether $Version uninstaller
`$ErrorActionPreference = 'Stop'
`$Dest = '$Dest'
`$BinDir = '$BinDir'
`$ideExe = '$ideExe'
foreach (`$lnk in @(
  (Join-Path ([Environment]::GetFolderPath('Desktop')) 'Aether IDE.lnk'),
  (Join-Path ([Environment]::GetFolderPath('Programs')) 'Aether IDE.lnk')
)) { if (Test-Path `$lnk) { Remove-Item `$lnk -Force } }
`$p = [Environment]::GetEnvironmentVariable('Path','User')
if (`$p -like "*`$BinDir*") {
  `$np = (`$p -split ';' | Where-Object { `$_ -and (`$_ -ne `$BinDir) }) -join ';'
  [Environment]::SetEnvironmentVariable('Path', `$np, 'User')
}
if (Test-Path `$Dest) { Remove-Item `$Dest -Recurse -Force }
Write-Host 'Aether 已卸载。'
"@
Set-Content -Path (Join-Path $Dest "Uninstall-Aether.ps1") -Value $uninst -Encoding UTF8

# Mirror to GrokBot-Apps if requested / exists parent
if ($GrokBotApps) {
  $rt = Join-Path $GrokBotApps "运行时"
  try {
    New-Item -ItemType Directory -Force -Path $rt | Out-Null
    Copy-Item (Join-Path $BinDir "aether.exe") (Join-Path $rt "aether.exe") -Force
    Copy-Item (Join-Path $Dest "Uninstall-Aether.ps1") (Join-Path $GrokBotApps "Uninstall-Aether.ps1") -Force -ErrorAction SilentlyContinue
    @"
Aether $Version
安装目录: $Dest
IDE: $ideExe
命令行: $BinDir\aether.exe
卸载: powershell -ExecutionPolicy Bypass -File "$Dest\Uninstall-Aether.ps1"
"@ | Set-Content -Encoding UTF8 (Join-Path $GrokBotApps "安装说明-0.5.1.txt")
    Write-Host "已同步运行时到 $rt"
  } catch {
    Write-Host "跳过 GrokBot-Apps 同步: $_"
  }
}

Write-Host ""
& (Join-Path $BinDir "aether.exe") version
& (Join-Path $BinDir "aether.exe") doctor
Write-Host ""
Write-Host "安装完成。" -ForegroundColor Green
Write-Host "  IDE:   $ideExe"
Write-Host "  命令:  aether  （新开终端后生效）"
Write-Host "  卸载:  powershell -ExecutionPolicy Bypass -File `"$Dest\Uninstall-Aether.ps1`""
