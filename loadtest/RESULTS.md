# Measured load results

Run on 2026-11-15. Server and k6 on the same machine: 12 logical cores,
15.4 GB RAM, PostgreSQL 18 local, Go 1.27. All traffic over loopback
(`127.0.0.1:8090`, keep-alive on).

## Race / correctness

`loadtest/concurrency.js` after `loadtest/reset.sql` (stock 100, no orders):

| Outcome | Count |
|---|---|
| HTTP 201 | 100 |
| HTTP 409 | 400 |
| other | 0 |

`SELECT stock FROM products WHERE id = 1` -> `0`,
`SELECT count(*) FROM orders` -> `100`. No oversell, no 5xx.

## Reads: `GET /health` + `GET /orders/{id}` (50/50)

Each tier is a fixed 20 s constant-arrival-rate run.

| Target req/s | Achieved | 5xx / error | p95 | p99 |
|---|---|---|---|---|
| 2,000 | 1,999.9 | 0.00% | 615 µs | 1.02 ms |
| 5,000 | 4,999.7 | 0.00% | 633 µs | 1.24 ms |
| 10,000 | 9,992.8 | 0.00% | 770 µs | 3.10 ms |
| 20,000 | 19,404 | 0.08% | 30.8 ms | 57.0 ms |

`GET /health` alone, ramping VUs: ~43,000 req/s with 0 failures, so the HTTP
layer is not the constraint. Reads are effectively free until the generator
itself (k6 on the same box) starts dropping iterations around 20k req/s.

## Writes: `POST /orders`

| Target req/s | Achieved | 201 | 5xx | p95 |
|---|---|---|---|---|
| 500 | 500.0 | 100% | 0.00% | 1.96 ms |
| 1,500 | 974 | 98.5% | 1.52% | 2.14 s |
| 3,000 | 1,090 | 87.5% | 12.48% | 3.00 s |

Ramped profile (`stress-write.js`, 5 -> 600 req/s) finished with 0 errors and
p95 2.99 ms, so everything up to 600 orders/s is comfortable.

Between 600 and 1,500 req/s the write path falls apart: throughput flattens
around **1,000 orders/s**, latency jumps to seconds, and the server starts
answering `503 database timeout` because each request only gets a 3 s context.
Server logs show `context deadline exceeded`, i.e. requests queued behind the
`db.SetMaxOpenConns(16)` pool.

The flat ~1,000 commits/s is the shape of a synchronous PostgreSQL commit
(WAL fsync per COMMIT) on local storage, not a Go bottleneck. Each order is two
statements plus a commit, so this is one durability flush per order.

## Write path after the single-statement change

`POST /orders` now does the stock check, decrement and insert in one CTE
statement instead of `BEGIN` + `UPDATE` + `INSERT` + `COMMIT`, and the pool
default dropped from 16 to 8 connections. Same machine, same 20 s runs:

| Target req/s | Before | After | Before 5xx | After 5xx | Before p95 | After p95 |
|---|---|---|---|---|---|---|
| 500 | 500 | 500 | 0.00% | 0.00% | 1.96 ms | 2.12 ms |
| 1,500 | 974 | 1,452 | 1.52% | **0.00%** | 2.14 s | 0.56 s |
| 3,000 | 1,090 | 1,487 | 12.48% | 4.56% | 3.00 s | 2.90 s |

The ceiling moved from ~1,000 to ~1,500 orders/s, and 1,500 req/s is now
error-free instead of returning 503s. What is left is the WAL fsync per order.
The race check still returns exactly 100 x 201 / 400 x 409 with stock 0.

## Practical conclusions

- Reads: >10k req/s with sub-millisecond p95 on this hardware.
- Writes: ~1,500 req/s, up from ~1,000; ~600/s is the comfortable range.
- Raising the write ceiling further means fewer commits (batched orders) or
  relaxing `synchronous_commit`, not more Go tuning.
- The 500-request race check passes exactly as the README predicts.
- See `deploy/` for Postgres settings and a systemd unit sized for 1 vCPU / 1 GB.

## Windows caveat

A single Go `http.Server` on Windows refuses new TCP connections when about
400+ arrive in the same instant (roughly 44% refusals at exactly 500). That is a
local listener artifact, not API behaviour: with connections spread over a
second the same server handles tens of thousands of requests per second.
`loadtest/concurrency.js` therefore staggers its 500 POSTs over ~1 s in five waves.
