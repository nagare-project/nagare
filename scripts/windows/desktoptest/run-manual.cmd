@echo off
rem nagare Windows desktop window test: manual checks (see README).
setlocal
cd /d "%~dp0"
rem Double-clicking inside the zip only extracts this one file to a temp folder: the script and the test build are missing.
if not exist "%~dp0run-wintest.ps1" goto notextracted
if not exist "%~dp0nagare\nagare.exe" goto notextracted
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0run-wintest.ps1" -Manual %*
set rc=%errorlevel%
rem 0 = all checks passed, 2 = finished but some checks failed. In both cases the script has already
rem shown the summary and waited for Enter, so do not pause a second time.
if "%rc%"=="0" exit /b 0
if "%rc%"=="2" exit /b 2
echo.
echo The manual run did not finish (exit code %rc%). See the messages above.
pause
exit /b %rc%

:notextracted
rem English only: Chinese text in a .cmd file is not reliable across Windows 10 code pages (the README says it in Chinese).
echo.
echo Please extract the whole zip first (right-click it, Extract All), then double-click run-manual.cmd in the extracted folder.
echo.
pause
exit /b 1
