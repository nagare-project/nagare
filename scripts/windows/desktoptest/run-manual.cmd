@echo off
rem nagare Windows desktop window test: manual checks (see README).
cd /d "%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0run-wintest.ps1" -Manual %*
