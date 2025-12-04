@echo off
REM Run chat stress test in Docker with inspector web exposed
REM Inspector web UI will be available at http://localhost:8080
REM Inspector gRPC will be available at localhost:8081

echo Starting stress test in Docker...
echo Inspector web UI will be available at http://localhost:8080
echo Press Ctrl+C to stop the test
echo.

docker run -it --rm -p 8080:8080 -p 8081:8081 -v %CD%:/app -w /app xiaonanln/goverse:dev python3 tests/samples/chat/stress/stress_test.py %*