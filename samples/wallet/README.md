# Wallet Sample

End-to-end demonstration of Goverse's **reliable-call** feature:
client-driven exactly-once semantics, even when the client retries after
timeouts or network hiccups.

## What it demonstrates

- A `Wallet` object with `Deposit`, `Withdraw`, and `GetBalance` methods.
- Clients drive all state changes via `reliable_call_object(call_id, ...)`
  so that a retry with the same `call_id` returns the cached outcome
  instead of applying the operation twice.
- A stress test that **injects mid-flight timeout aborts** (client
  cancels with a random 2–20 ms deadline) and then retries the same
  request with a normal timeout. Because the client cannot tell whether
  the first attempt landed, the reliable-call dedup is the only thing
  keeping balances correct.

Two invariants are asserted:

1. **In-flight**: for each retried call, the retry's `(ok, balance)`
   must match the first response. Any mismatch is a violation.
2. **End-of-run**: for every wallet, server-reported balance must equal
   the client ledger (`sum of applied deposits − sum of applied
   withdrawals`). Any drift means a call was applied more than once.

## Prerequisites

- Go 1.21+ and Python 3.8+ with `grpcio`, `grpcio-tools`, `pyyaml`.
- `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` on `PATH`.
- **Postgres** running on `localhost:5432` with the `goverse` user (the
  reliable-call dedup state is persisted there — required, not
  optional). The repo's `docker-compose.yml` sets this up:
  ```bash
  docker compose up -d postgres etcd
  ```
- **etcd** running on `localhost:2379` (same `docker-compose`).

## Quick start

From the repo root:

```bash
# 1. Generate proto artifacts (wallet.pb.go, wallet_pb2.py, ...).
./script/compile-proto.sh

# 2. Initialise the Postgres schema used by reliable calls.
go run ./cmd/pgadmin --config samples/wallet/stress_config.yml init

# 3. Run the stress test (60 s, 8 clients, fault injection on).
python3 samples/wallet/stress_test.py --duration 60 --clients 8
```

The script builds `/tmp/wallet_server`, starts 1 inspector + 3 wallet
nodes + 2 gates from `stress_config.yml`, drives the clients, then
shuts the cluster down and runs the conservation check.

Successful output ends with:

```
calls=...  retries=...  fault_injects=...  rpc_errors=0  idempotency_mismatches=0
✅ wallet-000: balance=...
...
✅ wallet stress test passed
```

## Running longer / heavier

```bash
ulimit -n 65536   # macOS default of 256 is too low for many clients
python3 samples/wallet/stress_test.py --duration 3600 --clients 100
```

Tuning knobs (constants at the top of `stress_test.py`):

- `NUM_WALLETS` — how many wallet IDs clients spread operations across.
- `RETRY_PROB` — probability of replaying a completed call.
- `FAULT_INJECT_PROB`, `FAULT_INJECT_MIN/MAX_TIMEOUT` — how often and
  how aggressively to cancel in-flight calls.

## Files

- `proto/wallet.proto` — `Deposit` / `Withdraw` / `GetBalance` messages.
- `server/wallet.go`, `server/main.go` — the Go server.
- `stress_config.yml` — 1 inspector + 3 nodes + 2 gates, Postgres wiring.
- `WalletServer.py` — subprocess wrapper used by `stress_test.py`.
- `stress_test.py` — the correctness stress driver.
