@echo off
setlocal enabledelayedexpansion
set "LOGFILE=test-chat.log"
echo Running chat tests... > "%LOGFILE%"
docker run --rm -v %CD%:/app goverse-dev /app/script/docker/test-chat.sh %* 2>&1 | powershell -Command "$input | ForEach-Object { Write-Host $_; Add-Content -Path '%LOGFILE%' -Value $_ }"
exit /b %ERRORLEVEL%
