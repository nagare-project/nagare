; nagare Windows 安装程序（NSIS 3 / MUI2）。由 scripts/windows/build-installer.sh 编译。
; 编译时必须传入：/DVERSION=<版本> /DVERSION_NUMERIC=<a.b.c.d> /DSRCDIR=<待打包目录>
;                 /DICON=<nagare.ico> /DOUTFILE=<输出 exe>
; 设计要点：
;   - 按用户安装（RequestExecutionLevel user）：装到 %LOCALAPPDATA%\Programs\nagare，
;     不需要管理员、不弹 UAC；卸载信息写 HKCU
;   - 无代码签名：SmartScreen 会拦一次，README 有「更多信息 → 仍要运行」说明
;   - 覆盖安装前先试删旧 nagare.exe，删不掉说明还在运行 —— 提示从托盘退出
Unicode true
!include "MUI2.nsh"
!include "FileFunc.nsh"

!ifndef VERSION
  !error "缺少 /DVERSION=<版本>"
!endif
!ifndef VERSION_NUMERIC
  !error "缺少 /DVERSION_NUMERIC=<a.b.c.d>"
!endif
!ifndef SRCDIR
  !error "缺少 /DSRCDIR=<待打包目录>"
!endif
!ifndef ICON
  !error "缺少 /DICON=<nagare.ico>"
!endif
!ifndef OUTFILE
  !define OUTFILE "nagare-${VERSION}_Windows_x86_64-setup.exe"
!endif

!define APP_NAME "nagare"
!define PUBLISHER "nagare project"
!define HOMEPAGE "https://github.com/nagare-project/nagare"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}"
!define APP_KEY "Software\${APP_NAME}"

Name "${APP_NAME}"
OutFile "${OUTFILE}"
RequestExecutionLevel user
InstallDir "$LOCALAPPDATA\Programs\${APP_NAME}"
InstallDirRegKey HKCU "${APP_KEY}" "InstallDir"
SetCompressor /SOLID lzma
BrandingText "${APP_NAME} ${VERSION}"

; ---- MUI 外观与页面 ----
!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\nagare.exe"
!define MUI_FINISHPAGE_RUN_TEXT "$(RunNagare)"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${SRCDIR}/LICENSE.txt"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; 第一个语言是默认语言
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"

LangString RunNagare ${LANG_SIMPCHINESE} "运行 nagare"
LangString RunNagare ${LANG_ENGLISH} "Run nagare"
LangString StillRunning ${LANG_SIMPCHINESE} "nagare 正在运行。$\r$\n$\r$\n请先在系统托盘右键 nagare 图标选择「退出」，然后点「重试」。"
LangString StillRunning ${LANG_ENGLISH} "nagare is still running.$\r$\n$\r$\nQuit it from the tray icon first, then click Retry."
LangString AbortRunning ${LANG_SIMPCHINESE} "已取消：nagare 仍在运行。"
LangString AbortRunning ${LANG_ENGLISH} "Aborted: nagare is still running."
LangString UninstallShortcut ${LANG_SIMPCHINESE} "卸载 nagare"
LangString UninstallShortcut ${LANG_ENGLISH} "Uninstall nagare"

; ---- exe 版本信息（资源管理器属性页）----
VIProductVersion "${VERSION_NUMERIC}"
VIFileVersion "${VERSION_NUMERIC}"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "ProductName" "${APP_NAME}"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "CompanyName" "${PUBLISHER}"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "FileDescription" "${APP_NAME} 安装程序"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "LegalCopyright" "AGPL-3.0"

; ---- 安装 ----
; 试删旧 exe：nagare 常驻托盘，用户很可能忘了退出；删不掉就是在运行（FindWindow 对
; 无窗口进程不可靠，这条判断反而最准）。
!macro EnsureNotRunning
  retry:
    ClearErrors
    IfFileExists "$INSTDIR\nagare.exe" 0 proceed
    Delete "$INSTDIR\nagare.exe"
    IfErrors 0 proceed
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(StillRunning)" IDRETRY retry
    Abort "$(AbortRunning)"
  proceed:
!macroend

Section "nagare" SecMain
  SectionIn RO
  SetOutPath "$INSTDIR"
  !insertmacro EnsureNotRunning

  File "${SRCDIR}/nagare.exe"
  File "${SRCDIR}/LICENSE.txt"
  File "${SRCDIR}/THIRD_PARTY_NOTICES.md"
  ; 内置 mpv：nagare.exe 旁边的 mpv\mpv.exe，Go 侧按这个相对位置查找
  File /r "${SRCDIR}/mpv"

  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortCut "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk" "$INSTDIR\nagare.exe"
  CreateShortCut "$SMPROGRAMS\${APP_NAME}\$(UninstallShortcut).lnk" "$INSTDIR\uninstall.exe"
  CreateShortCut "$DESKTOP\${APP_NAME}.lnk" "$INSTDIR\nagare.exe"

  WriteRegStr HKCU "${APP_KEY}" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${APP_NAME}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${PUBLISHER}"
  WriteRegStr HKCU "${UNINST_KEY}" "URLInfoAbout" "${HOMEPAGE}"
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\nagare.exe"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
SectionEnd

; ---- 卸载 ----
; 只删安装目录与快捷方式；%AppData%\nagare 下的配置、token、媒体库状态保留。
Section "Uninstall"
  !insertmacro EnsureNotRunning

  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\THIRD_PARTY_NOTICES.md"
  RMDir /r "$INSTDIR\mpv"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${APP_NAME}\*.lnk"
  RMDir "$SMPROGRAMS\${APP_NAME}"
  Delete "$DESKTOP\${APP_NAME}.lnk"

  DeleteRegKey HKCU "${UNINST_KEY}"
  DeleteRegKey HKCU "${APP_KEY}"
SectionEnd
