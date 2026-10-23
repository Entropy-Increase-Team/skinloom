@echo off
rem SkinLoom (云织) one-click start for Windows.
rem Starts the backend and the bundled frontend on one port and opens the browser.
rem Data (database, config, uploads) is kept in the "data" folder next to this file.
chcp 65001 >nul
cd /d "%~dp0"
title SkinLoom
skinloom.exe up --open %*
if errorlevel 1 (
  echo.
  echo SkinLoom exited with an error. See the messages above.
  pause
)
