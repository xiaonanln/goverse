
@echo off
setlocal enabledelayedexpansion
set "LOGFILE=test-go.log"
echo Running Go tests... > "%LOGFILE%"
docker run --rm -v %CD%:/app goverse-dev /app/script/docker/test-go.sh %* 2>&1 | powershell -Command "$input | ForEach-Object { Write-Host $_; Add-Content -Path '%LOGFILE%' -Value $_ }"
exit /b %ERRORLEVEL%
