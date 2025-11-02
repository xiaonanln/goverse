#!/bin/bash
# Test script to verify reentrant behavior of start/stop scripts
# This tests the logic without actually running the services

set -euo pipefail

echo "========================================"
echo "Testing Script Reentrancy"
echo "========================================"
echo

# Test 1: Verify scripts have proper shebang and permissions
echo "Test 1: Checking script files exist and are executable..."
for script in start-etcd.sh start-postgres.sh stop-etcd.sh stop-postgres.sh; do
    if [ ! -f "script/docker/$script" ]; then
        echo "✗ FAIL: script/docker/$script does not exist"
        exit 1
    fi
    if [ ! -x "script/docker/$script" ]; then
        echo "✗ FAIL: script/docker/$script is not executable"
        exit 1
    fi
    
    # Check for proper shebang
    if ! head -n 1 "script/docker/$script" | grep -q "^#!/bin/bash"; then
        echo "✗ FAIL: script/docker/$script does not have proper shebang"
        exit 1
    fi
    
    # Check for reentrant comment
    if ! grep -q "reentrant" "script/docker/$script"; then
        echo "✗ FAIL: script/docker/$script does not mention reentrancy"
        exit 1
    fi
    
    echo "  ✓ script/docker/$script is valid"
done
echo "✓ PASS: All script files exist, are executable, and mention reentrancy"
echo

# Test 2: Verify start scripts check for already running services
echo "Test 2: Checking start scripts have reentrant logic..."
if ! grep -q "pgrep.*etcd" "script/docker/start-etcd.sh"; then
    echo "✗ FAIL: start-etcd.sh does not check if etcd is already running"
    exit 1
fi
echo "  ✓ start-etcd.sh checks if etcd is already running"

if ! grep -q "pg_isready" "script/docker/start-postgres.sh"; then
    echo "✗ FAIL: start-postgres.sh does not check if PostgreSQL is already running"
    exit 1
fi
echo "  ✓ start-postgres.sh checks if PostgreSQL is already running"
echo "✓ PASS: Start scripts have reentrant logic"
echo

# Test 3: Verify stop scripts check for not running services
echo "Test 3: Checking stop scripts have reentrant logic..."
if ! grep -q "pgrep.*etcd" "script/docker/stop-etcd.sh"; then
    echo "✗ FAIL: stop-etcd.sh does not check if etcd is running"
    exit 1
fi
echo "  ✓ stop-etcd.sh checks if etcd is running"

if ! grep -q "pg_isready" "script/docker/stop-postgres.sh"; then
    echo "✗ FAIL: stop-postgres.sh does not check if PostgreSQL is running"
    exit 1
fi
echo "  ✓ stop-postgres.sh checks if PostgreSQL is running"
echo "✓ PASS: Stop scripts have reentrant logic"
echo

# Test 4: Verify scripts use set -euo pipefail for safety
echo "Test 4: Checking scripts use proper error handling..."
for script in start-etcd.sh start-postgres.sh stop-etcd.sh stop-postgres.sh; do
    if ! grep -q "set -euo pipefail" "script/docker/$script"; then
        echo "✗ FAIL: script/docker/$script does not use 'set -euo pipefail'"
        exit 1
    fi
    echo "  ✓ script/docker/$script uses 'set -euo pipefail'"
done
echo "✓ PASS: All scripts use proper error handling"
echo

# Test 5: Verify scripts have proper exit codes
echo "Test 5: Checking scripts have exit statements..."
for script in start-etcd.sh start-postgres.sh stop-etcd.sh stop-postgres.sh; do
    if ! grep -q "exit 0" "script/docker/$script"; then
        echo "✗ FAIL: script/docker/$script does not have success exit"
        exit 1
    fi
    echo "  ✓ script/docker/$script has exit statements"
done
echo "✓ PASS: All scripts have proper exit codes"
echo

# Test 6: Verify shellcheck passes on all scripts
echo "Test 6: Running shellcheck on all scripts..."
if command -v shellcheck > /dev/null 2>&1; then
    if shellcheck script/docker/start-etcd.sh script/docker/start-postgres.sh script/docker/stop-etcd.sh script/docker/stop-postgres.sh; then
        echo "✓ PASS: All scripts pass shellcheck"
    else
        echo "✗ FAIL: shellcheck found issues"
        exit 1
    fi
else
    echo "⚠ SKIP: shellcheck not available"
fi
echo

echo "========================================"
echo "✓ All Tests Passed!"
echo "========================================"
echo
echo "Summary:"
echo "  - All scripts exist and are executable"
echo "  - All scripts mention reentrancy in comments"
echo "  - Start scripts check if services are already running"
echo "  - Stop scripts check if services are not running"
echo "  - All scripts use proper error handling (set -euo pipefail)"
echo "  - All scripts have proper exit codes"
echo "  - All scripts pass shellcheck (if available)"
echo
