@echo off
rem nagare Windows desktop window test: automated run (see README).
cd /d "%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0run-wintest.ps1" %*
