@echo off
setlocal
rem LiveMix Studio one-command Windows build.
rem Examples:
rem   scripts\build.bat
rem   scripts\build.bat -Configuration Release -Run
rem   scripts\build.bat -Configuration Release -Clean -QtPath C:\Qt\6.8.0\msvc2022_64

powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*
if errorlevel 1 (
    echo.
    echo Build failed.
    pause
)
endlocal
