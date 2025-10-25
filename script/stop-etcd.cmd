@echo off
REM Stop and remove etcd Docker container

echo Stopping etcd container...

docker stop pulse-etcd
docker rm pulse-etcd

if %ERRORLEVEL% EQU 0 (
    echo.
    echo ✓ etcd container stopped and removed
) else (
    echo.
    echo ✗ Failed to stop etcd container
    echo   Container might not be running
)
