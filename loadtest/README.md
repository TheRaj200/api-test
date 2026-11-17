# k6 load tests

Scripts for poking at this practice API. Requires [k6](https://k6.io/) and the
API running with a seeded `api_test` database.

```powershell
# terminal 1
$env:DATABASE_URL = "postgres://postgres:PASSWORD@localhost:5432/api_test?sslmode=disable"
$env:PORT = "8090"   # optional, defaults to 8080
go run ./cmd/api
```

If `127.0.0.1:8080` is already taken by PEMHTTPD (the Apache that ships with
PostgreSQL Enterprise tooling), point the API at another port with `PORT` and
pass the same address to k6 with `-e BASE_URL=...`.

| Script | What it measures |
|---|---|
| `concurrency.js` | README race check: 500 POSTs for the last 100 units. Expects 100 x 201, 400 x 409, stock 0. |
| `stress-read.js` | Ramped arrival rate 10 -> 3000 req/s on `GET /health` + `GET /orders/{id}`. Finds where latency and errors break. |
| `stress-write.js` | Ramped arrival rate 5 -> 600 req/s on `POST /orders`. Measures the DB transaction path. |
| `rate-read.js` | Fixed rate for one tier, `-e RATE=5000 -e DUR=20s`. Use it to walk up to the ceiling. |
| `rate-write.js` | Fixed write rate for one tier, same env knobs. |
| `writes-fixed.js` | Fixed write rate across one or many products (`-e PRODUCT_IDS=9001,9002,...`), with status and latency-bucket counters. Used by the diagnostic harness. |
| `burst.js` | Fixed number of `POST /orders` requests fired as fast as possible (`-e REQUESTS`, `-e VUS`), for the 100-unit oversell/burst check. |

The full write-path diagnostic harness (resource sampler, runners, SQL
snapshots and report) lives in [`diagnostics/`](diagnostics/README.md) and its
findings are in [`DIAGNOSTIC_REPORT.md`](DIAGNOSTIC_REPORT.md).

## Running

```powershell
# correctness first
psql $env:DATABASE_URL -f loadtest/reset.sql
k6 run -e BASE_URL=http://127.0.0.1:8090 loadtest/concurrency.js

# read capacity
k6 run -e BASE_URL=http://127.0.0.1:8090 loadtest/stress-read.js

# write capacity (top up stock first, otherwise everything is a fast 409)
psql $env:DATABASE_URL -f loadtest/restock-high.sql
k6 run -e BASE_URL=http://127.0.0.1:8090 loadtest/stress-write.js
```

`k6 run` exits with code 99 when a threshold is crossed (5xx rate > 5% or
p95 > 1s). For the stress scripts that is the expected outcome once the
machine saturates, not a broken test.

## Windows note

A single Go `http.Server` on Windows starts refusing new TCP connections when
roughly 400+ connections arrive in the same instant, which shows up as bogus
`connection refused` errors. Keep bursts under ~400, or ramp VUs over a second
or two, and the same server happily serves tens of thousands of requests per
second.
