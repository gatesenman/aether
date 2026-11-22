Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma
SetCompressorDictSize 64
!define APP "Aether IDE"
!define VER "0.6.0"
; Paths are relative to this .nsi file. Override at compile time, for example:
;   makensis -DSRC=/path/to/aether-ide-win32-x64 -DOUTFILE=/path/to/Aether-Setup-0.6.0-win32-x64.exe installer/nsis/aether.nsi
; On Windows, the same defines can be passed as /DSRC=... /DOUTFILE=...
!ifndef SRC
  !define SRC "../../release-0.6.0/aether-ide-win32-x64"
!endif
!ifndef OUTFILE
  !define OUTFILE "../../release-0.6.0/Aether-Setup-0.6.0-win32-x64.exe"
!endif
!define UK "Software\Microsoft\Windows\CurrentVersion\Uninstall\AetherIDE"
!include MUI2.nsh
!include WinMessages.nsh
Name "${APP} ${VER}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\Aether IDE"
InstallDirRegKey HKCU "${UK}" "InstallLocation"
RequestExecutionLevel user
BrandingText "Aether ${VER}"
VIProductVersion "0.6.0.0"
VIAddVersionKey /LANG=2052 "ProductName" "Aether IDE"
VIAddVersionKey /LANG=2052 "FileDescription" "Aether IDE 安装程序"
VIAddVersionKey /LANG=2052 "FileVersion" "0.6.0"
VIAddVersionKey /LANG=2052 "ProductVersion" "0.6.0"
VIAddVersionKey /LANG=2052 "CompanyName" "Aether Project"
VIAddVersionKey /LANG=2052 "LegalCopyright" "Aether Project"
!define MUI_ICON "aether.ico"
!define MUI_UNICON "aether.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\Aether IDE.exe"
!define MUI_FINISHPAGE_RUN_TEXT "启动 Aether IDE"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "license16.txt"
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

Section "Aether IDE（必需）" SecMain
  SectionIn RO
  SetOutPath "$INSTDIR"
  File /r "${SRC}/*"
  File "/oname=aether.ico" "aether.ico"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateDirectory "$SMPROGRAMS\Aether IDE"
  CreateShortCut "$SMPROGRAMS\Aether IDE\Aether IDE.lnk" "$INSTDIR\Aether IDE.exe" "" "$INSTDIR\aether.ico"
  CreateShortCut "$SMPROGRAMS\Aether IDE\卸载 Aether IDE.lnk" "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "${UK}" "DisplayName" "Aether IDE ${VER}"
  WriteRegStr HKCU "${UK}" "DisplayVersion" "${VER}"
  WriteRegStr HKCU "${UK}" "Publisher" "Aether Project"
  WriteRegStr HKCU "${UK}" "DisplayIcon" "$INSTDIR\aether.ico"
  WriteRegStr HKCU "${UK}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UK}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "${UK}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKCU "${UK}" "NoModify" 1
  WriteRegDWORD HKCU "${UK}" "NoRepair" 1
  WriteRegDWORD HKCU "${UK}" "EstimatedSize" 746926
SectionEnd

Section "桌面快捷方式" SecDesk
  CreateShortCut "$DESKTOP\Aether IDE.lnk" "$INSTDIR\Aether IDE.exe" "" "$INSTDIR\aether.ico"
SectionEnd

Section "将 aether 命令加入 PATH" SecPath
  ReadRegStr $0 HKCU "Environment" "Path"
  StrCmp $0 "" 0 +3
    StrCpy $0 "$INSTDIR\bin"
    Goto +2
  StrCpy $0 "$0;$INSTDIR\bin"
  WriteRegExpandStr HKCU "Environment" "Path" $0
  WriteRegStr HKCU "${UK}" "AddedPath" "$INSTDIR\bin"
  SendMessage ${HWND_BROADCAST} ${WM_WININICHANGE} 0 "STR:Environment" /TIMEOUT=2000
SectionEnd

LangString D1 2052 "Aether IDE 主程序与 aether 运行时。"
LangString D2 2052 "在桌面创建 Aether IDE 快捷方式。"
LangString D3 2052 "把 bin 目录加入当前用户 PATH，可在终端直接运行 aether。"
!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecMain} $(D1)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecDesk} $(D2)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecPath} $(D3)
!insertmacro MUI_FUNCTION_DESCRIPTION_END


; remove "$R1" substring (with leading ;) from PATH
Section "Uninstall"
  ReadRegStr $R1 HKCU "${UK}" "AddedPath"
  StrCmp $R1 "" nopath
  ReadRegStr $R0 HKCU "Environment" "Path"
  StrCpy $R2 ""          ; result
  StrCpy $R3 $R0         ; remaining
  loop:
    StrCmp $R3 "" done
    ; take up to next ;
    StrCpy $R4 0
    find:
      StrCpy $R5 $R3 1 $R4
      StrCmp $R5 "" got
      StrCmp $R5 ";" got
      IntOp $R4 $R4 + 1
      Goto find
    got:
    StrCpy $R6 $R3 $R4
    IntOp $R4 $R4 + 1
    StrCpy $R3 $R3 "" $R4
    StrCmp $R6 $R1 loop
    StrCmp $R6 "" loop
    StrCmp $R2 "" 0 +3
      StrCpy $R2 $R6
      Goto loop
    StrCpy $R2 "$R2;$R6"
    Goto loop
  done:
  WriteRegExpandStr HKCU "Environment" "Path" $R2
  SendMessage ${HWND_BROADCAST} ${WM_WININICHANGE} 0 "STR:Environment" /TIMEOUT=2000
  nopath:
  Delete "$DESKTOP\Aether IDE.lnk"
  RMDir /r "$SMPROGRAMS\Aether IDE"
  RMDir /r "$INSTDIR"
  DeleteRegKey HKCU "${UK}"
SectionEnd
