@echo off
rem nagare Windows desktop window test: manual checks (see README).
setlocal
cd /d "%~dp0"
rem Double-clicking inside the zip only extracts this one file to a temp folder: the script and the test build are missing.
if not exist "%~dp0run-wintest.ps1" goto notextracted
if not exist "%~dp0nagare\nagare.exe" goto notextracted
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0run-wintest.ps1" -Manual %*
if errorlevel 1 (
  echo.
  echo The manual run did not finish cleanly. See the messages above.
  pause
)
exit /b %errorlevel%

:notextracted
chcp 65001 >nul
echo.
echo 请先把整个测试包解压出来：右键 zip，选「全部解压缩」，再到解压出来的文件夹里双击 run-manual.cmd。
echo Please extract the whole zip first (right-click it, Extract All), then run run-manual.cmd from the extracted folder.
echo.
pause
exit /b 1
