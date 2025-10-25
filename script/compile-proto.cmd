@echo off
REM Compile protobuf files using Docker
echo Compiling protobuf files...
docker run -it --rm -v %CD%:/app dev ./script/compile-proto.sh
if %ERRORLEVEL% equ 0 (
    echo Protobuf compilation completed successfully.
) else (
    echo Protobuf compilation failed with error code %ERRORLEVEL%.
    exit /b %ERRORLEVEL%
)