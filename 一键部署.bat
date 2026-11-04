@echo off
setlocal
if defined CANVAS_DEPLOY_WINDOW goto inside
set "CANVAS_DEPLOY_WINDOW=1"
start "Canvas deploy" "%ComSpec%" /k call "%~f0" %*
exit /b 0

:inside
set "SCRIPT_DIR=%~dp0"
set "DEPLOY_SCRIPT=%SCRIPT_DIR%deploy\deploy.ps1"
if not exist "%DEPLOY_SCRIPT%" (
  echo [ERROR] Cannot find deploy script: "%DEPLOY_SCRIPT%"
  set "EXIT_CODE=1"
  goto finish
)
where powershell.exe >nul 2>&1
if errorlevel 1 (
  echo [ERROR] Windows PowerShell is not available.
  set "EXIT_CODE=1"
  goto finish
)
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%DEPLOY_SCRIPT%" %*
set "EXIT_CODE=%ERRORLEVEL%"

:finish
echo.
if "%EXIT_CODE%"=="0" (
  echo Operation completed successfully.
) else (
  echo Operation failed with exit code %EXIT_CODE%.
)
echo.
pause
exit /b %EXIT_CODE%
