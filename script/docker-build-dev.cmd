@echo off
setlocal enabledelayedexpansion

REM Configuration
set IMAGE_NAME=goverse-dev
set DOCKERFILE=docker/Dockerfile.dev
set BUILD_CONTEXT=.

echo Building Docker image: %IMAGE_NAME%

REM Check if Dockerfile exists
if not exist "%DOCKERFILE%" (
    echo Error: Dockerfile not found at %DOCKERFILE%
    exit /b 1
)

REM Build the image
docker build -f "%DOCKERFILE%" -t "%IMAGE_NAME%" "%BUILD_CONTEXT%"
if %errorlevel% neq 0 (
    echo Failed to build %IMAGE_NAME%
    exit /b 1
)

echo Successfully built %IMAGE_NAME%
exit /b 0
