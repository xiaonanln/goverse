@echo off
REM Start etcd in Docker container for local development

echo Starting etcd container...

docker run -d ^
  -p 2379:2379 ^
  --name goverse-etcd ^
  quay.io/coreos/etcd:v3.5.15 ^
  /usr/local/bin/etcd ^
  --listen-client-urls http://0.0.0.0:2379 ^
  --advertise-client-urls http://0.0.0.0:2379

if %ERRORLEVEL% EQU 0 (
    echo.
    echo ✓ etcd container started successfully
    echo   Container name: goverse-etcd
    echo   Client endpoint: http://localhost:2379
    echo.
    echo To stop: docker stop goverse-etcd
    echo To remove: docker rm goverse-etcd
) else (
    echo.
    echo ✗ Failed to start etcd container
    echo   Make sure Docker is running
    echo   Or the container might already exist - try: docker rm -f goverse-etcd
)
