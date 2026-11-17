# POST /orders write-path diagnostic report

**Date:** 2026-11-17
**Endpoint:** `POST /orders` on the deployed API
**Verdict in one line:** at 1,000 target RPS on a single product the API collapses because every
order updates the same `products.stock` row; spreading the identical load over four products
keeps 1,200 RPS at p95 5.08 ms with zero errors. The next wall after that is the one durable
WAL fsync per order.

---

## 1. API contract verified

| Request | Result | Meaning |
|---|---|---|
| `POST /orders` with `{"product_id":9001,"quantity":1}` | `201` + `{"id":N,"product_id":9001,"quantity":1}` | order committed |
| `POST /orders` when stock is exhausted | `409` + `{"error":"product unavailable or insufficient stock"}` | valid out-of-stock |
| `POST /orders` when the 3 s DB context expires | `503` + `{"error":"database timeout"}` | server-side database timeout, not a stock answer |
| `POST /orders` with invalid body | `400` + `{"error":"invalid order"}` | rejected |
| `GET /orders/{id}` missing | `404` + `{"error":"order not found"}` | not found |

A successful order is therefore exactly **HTTP 201 with an `id`**, and out-of-stock is exactly
**HTTP 409**. The handler applies a 3 s query timeout (`internal/api/api.go`), and the store
performs the stock check, decrement and insert in **one statement**
(`internal/store/store.go`), so oversell protection is a single-statement guarantee.

The 100-of-500 race check was not re-run in this matrix; it was verified earlier on the same
deployment (100 × 201, 400 × 409, stock 0, orders 100) and the burst test below re-confirms the
same invariant at 500–4,000 concurrent requests.

## 2. Deployed commit and configuration

| Item | Value |
|---|---|
| Host | `ip-172-31-8-16.ap-southeast-2.compute.internal` (ap-southeast-2), Amazon Linux 2023.12, kernel 6.18.62 |
| Size | **2 vCPU, 3,815 MB RAM**, 8 GB gp3 disk, SELinux Permissive |
| Deployed app | static binary `/opt/api-test/api-test`, SHA-256 `4bf4b4fda3da281d3f230198c422dcf5cbafcbe0cbd6b5778c20ad8ab5695486` |
| App commit | built from `0a33b39` (last commit touching `cmd/` or `internal/`); repository HEAD at measurement `d2924bb`, which only adds docs/load-test files |
| Service | `api-test`, active + enabled, `PORT=8080`, `DB_MAX_OPEN_CONNS=6`, `GOMEMLIMIT=128MiB`, `MemoryMax=256M`, `LimitNOFILE=4096`, `Restart=always` |
| PostgreSQL | 16.15, `synchronous_commit=on`, `fsync=on`, `full_page_writes=on`, `wal_level=replica`, `shared_buffers=128MB`, `max_connections=100`, `max_wal_size=1GB`, `checkpoint_timeout=300s` |
| Pool | Go `database/sql` max **6** open / 6 idle connections |
| Schema | `products(id, stock)`, `orders(id identity, product_id, quantity)`, `orders_product_id_idx`, FK to products |

The application and database settings were held fixed for every run. One **instrumentation-only**
setting was enabled once before the matrix and kept constant: `track_wal_io_timing=on`. PostgreSQL
does not populate `wal_sync_time`/`wal_write_time` without it. It adds timing calls, not durability
or batching behaviour, and it is recorded in every run's `meta.txt`.

## 3. Method

### Load generator

No separate load-generator host was available, so k6 v2.4.0 ran **on the same 2 vCPU instance**
and its CPU is reported separately. Every rate/CPU number in this report is therefore a
**shared-host measurement**: k6 competes with the API and PostgreSQL for the two cores. Traffic
uses loopback (`127.0.0.1:8080`), so no WAN latency is included.

### Fixtures

- Single-product series: product **9001**, stock reset to **5,000,000**.
- Spread series: products **9001, 9002, 9003, 9004**, each reset to **5,000,000**; every request
  chooses one uniformly at random.
- Burst series: product **9201**, reset to exactly **100**; every request orders quantity 1.
- Demo product 1 was never modified. `setup-fixtures.sql` deletes orders for products 9000–9999
  and resets the stocks before **every** run, so each run starts with fresh fixtures.

### Runs

- 10 s warm-up at the same target rate, then **60 s measured**, then a before/after DB snapshot.
- Single and spread: 500, 650, 800, 1,000 target RPS. The steps around the knee were repeated
  (`-b`). Two extra points were added after the first pass: single 900 (to bracket the knee) and
  spread 1,200 (to see the next wall).
- Bursts: 500, 1,000, 2,000 and 4,000 requests, all fired as fast as possible, fresh 100-unit
  stock each time. A 10 Hz `/health` probe ran across each burst to measure latency recovery.
- The judge deadline is unknown, so the comparison thresholds are explicitly **100 ms, 500 ms
  and 1 s**.

### Definitions

| Field | Definition |
|---|---|
| Scheduled | iterations k6 intended to run (`rate × duration`) |
| Started / completed | k6 `iterations` / `http_reqs` (one request per iteration; equal in these runs) |
| Dropped | scheduled iterations k6 never started (client-side queueing) |
| Achieved/s | completed HTTP requests ÷ measured wall time |
| Committed | rows added to `orders` for the test product(s), from before/after snapshots |
| Committed/s | committed rows ÷ measured wall time |
| 409 | valid out-of-stock responses |
| 5xx | here always `503 database timeout` from the handler's 3 s context |
| Timeout | k6 client timeout; client timeout was 10 s, so server 503s arrive first and this stays 0 |
| Pool conns / active | `pg_stat_activity` client backends for `apitest`, sampled at 1 Hz |
| Lock waits | `pg_stat_activity` rows with `wait_event_type='Lock'`, sampled at 1 Hz |
| WAL fsync/s, WAL sync ms | `pg_stat_wal.wal_sync` and `wal_sync_time` deltas over the measured window |
| Disk write ms | `/proc/diskstats` `time_writing ÷ writes_completed` deltas |

Resource sampler: `/proc` tick counters and RSS for the API, PostgreSQL and k6; `pg_stat_activity`,
`pg_locks`, `pg_stat_database`, `pg_stat_wal`; `/proc/diskstats`, all once per second.

**Connection-pool wait is not directly observable.** The API does not export `sql.DBStats`
(`WaitCount`, `WaitDuration`). This report uses the `apitest` backend count versus the pool limit
of 6 as the inference: 6/6 with requests queueing in `database/sql` is treated as a full pool.

## 4. Reproduce

The harness is in `loadtest/diagnostics/` and was run from `/tmp/diag` on the instance.

```bash
# 0. upload harness (scripts + k6 files), then capture config
bash /tmp/diag/env.sh | tee /tmp/diag/results/env.txt

# 1. single product, then four products (warm-up 10 s, measured 60 s)
for r in 500 650 800 1000; do bash /tmp/diag/run_case.sh single-$r $r 9001; done
for r in 500 650 800 1000; do bash /tmp/diag/run_case.sh spread-$r $r 9001,9002,9003,9004; done

# 2. repeat around the knee, plus the 900/1200 bracket points
for r in 650 800 1000; do bash /tmp/diag/run_case.sh single-$r-b $r 9001; done
for r in 650 800 1000; do bash /tmp/diag/run_case.sh spread-$r-b $r 9001,9002,9003,9004; done
bash /tmp/diag/run_case.sh single-900 900 9001
bash /tmp/diag/run_case.sh spread-1200 1200 9001,9002,9003,9004

# 3. bursts on exactly 100 units
bash /tmp/diag/run_burst.sh burst-500 500 100
bash /tmp/diag/run_burst.sh burst-1000 1000 200
bash /tmp/diag/run_burst.sh burst-2000 2000 400
bash /tmp/diag/run_burst.sh burst-4000 4000 800

# 4. aggregate to results.csv and the tables below
python3 /tmp/diag/analyze.py /tmp/diag/results
```

A single case can be re-run against another deployment by changing the last
`run_case.sh` argument (`BASE_URL`), e.g. from a separate generator host:

```bash
BASE_URL=http://<deployed-host>:8080 k6 run -e RATE=1000 -e DUR=60s \
  -e PRODUCT_IDS=9001 loadtest/writes-fixed.js
```

No rate limiting is configured anywhere in this path, and none was added.

## 5. Results

### 5.1 Single product (9001)

| Run | Target | Scheduled | Started | 201 | Achieved/s | Committed | Committed/s | 409 | 503 | Other | Dropped | p50 ms | p95 ms | p99 ms | <100 ms | <500 ms | <1 s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| single-500 | 500 | 30,001 | 30,001 | 30,001 | 499.99 | 30,001 | 499.99 | 0 | 0 | 0 | 0 | 1.44 | 1.69 | 2.65 | 30,001 | 30,001 | 30,001 |
| single-650 | 650 | 39,000 | 39,000 | 39,000 | 649.98 | 39,000 | 649.98 | 0 | 0 | 0 | 0 | 1.44 | 2.01 | 6.28 | 39,000 | 39,000 | 39,000 |
| single-650-b | 650 | 39,001 | 39,001 | 39,001 | 649.99 | 39,001 | 649.99 | 0 | 0 | 0 | 0 | 1.44 | 1.86 | 5.07 | 39,001 | 39,001 | 39,001 |
| single-800 | 800 | 48,001 | 48,001 | 48,001 | 799.97 | 48,001 | 799.97 | 0 | 0 | 0 | 0 | 1.62 | 3.79 | 13.80 | 47,999 | 48,001 | 48,001 |
| single-800-b | 800 | 48,001 | 48,001 | 48,001 | 799.98 | 48,001 | 799.98 | 0 | 0 | 0 | 0 | 1.58 | 3.38 | 13.67 | 48,001 | 48,001 | 48,001 |
| **single-900** | 900 | 54,000 | 54,000 | 53,998 | 894.52 | 53,998 | 894.48 | 0 | **2** | 0 | 0 | **139.45** | **688.22** | **1,138.29** | **21,331** | 48,369 | 53,101 |
| **single-1000** | 1,000 | 60,001 | 59,057 | 54,026 | 963.33 | 54,037 | 881.44 | 0 | **5,031** | 0 | **944** | **782.92** | **3,000.51** | **3,001.10** | **5,557** | 21,723 | 34,385 |
| **single-1000-b** | 1,000 | 60,001 | 59,060 | 54,061 | 963.54 | 54,067 | 882.08 | 0 | **4,999** | 0 | **941** | **784.34** | **3,000.51** | **3,001.09** | **5,493** | 21,645 | 34,308 |

### 5.2 Same load spread over four products

| Run | Target | Scheduled | Started | 201 | Achieved/s | Committed | Committed/s | 409 | 503 | Other | Dropped | p50 ms | p95 ms | p99 ms | <100 ms | <500 ms | <1 s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| spread-500 | 500 | 30,001 | 30,001 | 30,001 | 499.99 | 30,001 | 499.99 | 0 | 0 | 0 | 0 | 1.43 | 1.66 | 2.39 | 30,001 | 30,001 | 30,001 |
| spread-650 | 650 | 39,001 | 39,001 | 39,001 | 649.99 | 39,001 | 649.99 | 0 | 0 | 0 | 0 | 1.43 | 1.77 | 3.41 | 39,001 | 39,001 | 39,001 |
| spread-650-b | 650 | 39,001 | 39,001 | 39,001 | 649.99 | 39,001 | 649.99 | 0 | 0 | 0 | 0 | 1.43 | 1.77 | 3.48 | 39,001 | 39,001 | 39,001 |
| spread-800 | 800 | 48,001 | 48,001 | 48,001 | 799.98 | 48,001 | 799.98 | 0 | 0 | 0 | 0 | 1.51 | 2.33 | 4.13 | 48,001 | 48,001 | 48,001 |
| spread-800-b | 800 | 48,000 | 48,000 | 48,000 | 799.96 | 48,000 | 799.96 | 0 | 0 | 0 | 0 | 1.51 | 2.34 | 4.26 | 48,000 | 48,000 | 48,000 |
| spread-1000 | 1,000 | 60,001 | 60,001 | 60,001 | 999.97 | 60,001 | 999.97 | 0 | 0 | 0 | 0 | 1.88 | 2.86 | 9.56 | 60,001 | 60,001 | 60,001 |
| spread-1000-b | 1,000 | 60,001 | 60,001 | 60,001 | 999.96 | 60,001 | 999.96 | 0 | 0 | 0 | 0 | 1.88 | 2.83 | 7.67 | 60,001 | 60,001 | 60,001 |
| spread-1200 | 1,200 | 72,001 | 72,001 | 72,001 | 1,199.93 | 72,001 | 1,199.93 | 0 | 0 | 0 | 0 | 1.96 | 5.08 | 15.32 | 72,001 | 72,001 | 72,001 |

### 5.3 Bursts on exactly 100 units (product 9201)

All four bursts committed **exactly 100 orders**, consumed exactly 100 units, and left stock 0.
Every other request that reached the API was a valid 409. No 5xx, no client timeout, no network
error, nothing dropped.

| Burst | Requests | Duration | 201 | 409 | 5xx / timeout | Dropped | p50 ms | p95 ms | p99 ms | <100 ms | <500 ms | <1 s | Final stock | `/health` max | Recovery |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| burst-500 | 500 | **0.22 s** | 100 | 400 | 0 / 0 | 0 | 25.68 | 136.93 | 168.26 | 442 | 500 | 500 | 0 | 10.89 ms | 344 ms |
| burst-1000 | 1,000 | **0.34 s** | 100 | 900 | 0 / 0 | 0 | 41.21 | 168.92 | 258.54 | 812 | 1,000 | 1,000 | 0 | 0.55 ms | 232 ms |
| burst-2000 | 2,000 | **0.67 s** | 100 | 1,900 | 0 / 0 | 0 | 88.04 | 291.49 | 399.18 | 1,095 | 1,997 | 2,000 | 0 | 6.17 ms | 237 ms |
| burst-4000 | 4,000 | **1.22 s** | 100 | 3,900 | 0 / 0 | 0 | 149.18 | 521.23 | 756.07 | 1,399 | 3,766 | 3,995 | 0 | 4.67 ms | 230 ms |

The burst duration is wall time from k6 start to the last response (k6 waits for every response,
so the queue is drained before it exits). The `/health` probe stayed at 0.55–10.89 ms throughout,
including during the 4,000-request burst: the Go process and HTTP path stayed responsive while the
order/DB path queued. That rules out an HTTP-server or CPU cliff as the burst limit.

In every rate run, **started = completed** (one request per iteration), and k6 client timeouts,
network errors and unexpected statuses were **0**; every 5xx shown above is the handler's
`503 database timeout`.

Recovery is the time from the probe start until `/health` latency stayed below
`max(3 × post-burst baseline, 5 ms)`; probe resolution is 100 ms. The 230–344 ms values are
therefore an upper bound at 100 ms granularity, not a long tail.

### 5.4 Resource and wait measurements

| Run | API CPU avg/max % | PG CPU avg/max % | k6 CPU avg % | API RSS MB | PG RSS MB | Pool conns max | PG active max | Lock waits max | WAL fsync/s | WAL sync ms | Disk write ms | Load1 max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| single-500 | 10.7/12.0 | 14.1/16.0 | 16.9 | 20.8 | 264.3 | 6 | 1 | 1 | 494.4 | 0.940 | 0.927 | 1.7 |
| single-650 | 13.2/15.0 | 18.7/22.0 | 20.6 | 22.7 | 281.3 | 6 | 3 | 2 | 657.0 | 0.933 | 0.927 | 1.9 |
| single-650-b | 13.2/15.0 | 18.4/23.0 | 20.5 | 25.6 | 335.6 | 6 | 5 | 3 | 651.8 | 0.938 | 0.923 | 1.9 |
| single-800 | 15.7/17.0 | 25.0/28.0 | 25.6 | 25.1 | 295.9 | 6 | 6 | 4 | 803.9 | 0.933 | 0.924 | 2.0 |
| single-800-b | 16.0/18.0 | 25.2/29.0 | 25.8 | 27.2 | 310.1 | 6 | 6 | 5 | 792.7 | 0.937 | 0.922 | 2.5 |
| single-900 | 17.8/20.0 | 36.8/41.0 | 26.2 | 33.2 | 338.5 | 6 | 6 | 5 | 890.8 | 0.940 | 0.922 | 2.6 |
| single-1000 | 19.3/22.0 | 33.9/41.0 | 29.5 | 67.8 | 298.1 | 6 | 6 | 5 | 882.5 | 0.943 | 0.922 | 3.0 |
| single-1000-b | 19.1/22.0 | 34.8/41.0 | 29.3 | 67.9 | 308.0 | 6 | 6 | 5 | 881.1 | 0.943 | 0.923 | 3.3 |
| spread-500 | 10.4/12.0 | 13.8/16.0 | 16.7 | 47.3 | 275.9 | 6 | 2 | 0 | 504.2 | 0.935 | 0.930 | 2.4 |
| spread-650 | 13.1/15.0 | 18.0/20.0 | 20.5 | 25.6 | 292.5 | 6 | 2 | 1 | 649.1 | 0.931 | 0.929 | 1.5 |
| spread-650-b | 12.9/14.0 | 17.9/21.0 | 20.3 | 39.5 | 291.0 | 6 | 2 | 0 | 655.6 | 0.938 | 0.922 | 2.5 |
| spread-800 | 15.7/17.0 | 23.4/27.0 | 25.9 | 26.9 | 301.0 | 6 | 3 | 1 | 797.6 | 0.932 | 0.927 | 1.8 |
| spread-800-b | 15.6/17.0 | 23.3/26.0 | 25.8 | 27.6 | 324.0 | 6 | 3 | 1 | 793.1 | 0.938 | 0.920 | 1.8 |
| spread-1000 | 19.4/21.0 | 29.2/33.0 | 27.3 | 29.9 | 307.1 | 6 | 5 | 4 | 960.9 | 0.940 | 0.919 | 1.9 |
| spread-1000-b | 19.6/21.0 | 29.5/34.0 | 27.3 | 30.2 | 316.0 | 6 | 4 | 2 | 966.6 | 0.940 | 0.923 | 1.8 |
| spread-1200 | 21.7/24.0 | 35.9/40.0 | 30.7 | 32.7 | 364.3 | 6 | 6 | 3 | 986.3 | 0.944 | 0.921 | 2.6 |

Resource sampling was **not** run for the four burst cases; those cells are intentionally absent
rather than inferred. The memory available never dropped below 2.7 GB, so neither the API's 256 MB
`MemoryMax` nor the host RAM was a factor.

### 5.5 Stock reconciliation

| Group | 201 responses | Committed rows | Stock decrement | Match |
|---|---|---|---|---|
| single-500 / 650 / 650-b / 800 / 800-b | exactly equal to requests | equal | equal | yes |
| single-900 | 53,998 | 53,998 | 53,998 | yes |
| single-1000 | 54,026 | **54,037** | **54,037** | yes (DB), 11 more commits than 201s |
| single-1000-b | 54,061 | **54,067** | **54,067** | yes (DB), 6 more commits than 201s |
| spread-500…1200 (all) | exactly equal to requests | equal | equal | yes |
| burst-500/1000/2000/4000 | 100 each | 100 each | 100 each | yes, final stock 0 each |

Committed rows and stock decrement matched exactly in **every** run. In 14 of 16 rate runs the 201
count also matched exactly. In the two 1,000 RPS single-product collapses, 11 and 6 requests
(0.01–0.02 %) were committed even though the client received `503 database timeout`: the 3 s
context expired while the already-started commit landed. That is why committed orders are counted
from `orders`/`stock`, not from HTTP status, and it confirms no oversell and no lost stock.

### 5.6 First materially degrading step

**Single product: 900 target RPS is the first step that fails the 100 ms budget.** At 800 RPS p95
is 3.79 ms, p99 13.8 ms, 0 errors. At 900 RPS p50 jumps to 139 ms, p95 to 688 ms, p99 to 1.14 s,
only 39.5 % of requests finish within 100 ms, and two requests hit the 3 s server timeout. At
1,000 RPS the queue exceeds the 3 s timeout: 5,031 × 503, throughput falls to 881 committed/s,
and k6 drops 944 scheduled iterations because every VU is occupied by a slow request.

**Spread over four products: no material degradation through 1,200 RPS.** At 1,000 RPS the repeat
runs give p95 2.86/2.83 ms and 0 errors; at 1,200 RPS p95 is 5.08 ms, p99 15.32 ms, and 100 % of
requests finish within 100 ms. The only difference from the failing single-product run is which
`products` row is updated.

## 6. Bottleneck hypotheses, ranked by evidence

### 1. Same-row serialization in `UPDATE products SET stock = stock - $1` (dominant)

Every order updates one row for the product. PostgreSQL locks that row until the transaction
commits, so orders for one product form a single queue whose service time is approximately one
commit. At 800 RPS the queue is still short; at 900 RPS it is at the edge and p95 explodes; at
1,000 RPS requests wait past the 3 s handler timeout and return 503.

Evidence:

- Identical request shape, rate, duration and configuration: single-800 is clean, single-900
  p95 688 ms, spread-1000/1200 p95 2.9/5.1 ms.
- At single-900/1000, all **6/6 pool connections** are checked out and `pg_stat_activity` shows
  up to **5 lock waiters**; at spread-1000/1200 the pool is also full but lock waiters are 2–4 and
  latency stays low.
- The API, PostgreSQL and k6 CPUs stay at **≤24 %, ≤41 % and ≤31 %** during the collapse, so this
  is waiting, not CPU exhaustion.
- The 503s are exactly the handler's 3 s `context.DeadlineExceeded` path, not a crash or network
  error; k6 client timeouts are 0.

### 2. One durable WAL fsync per order (next wall, not the first one)

Every committed order writes WAL and, with `synchronous_commit=on`, waits for a durable flush.
The measured fsync latency is stable at **0.93–0.94 ms** across all rates, and the fsync rate
tracks the commit rate. At spread-1200 the sampler sees **986 fsync/s × 0.944 ms ≈ 931 ms of
flush time per second (≈93 % duty cycle)** while p95 is still 5 ms. Extrapolating, a single node
with this storage tops out near **1,000–1,100 fsyncs/s** before fsync queueing becomes visible.

Evidence:

- `wal_sync_ms` is constant (0.931–0.944 ms) from 500 to 1,200 RPS, so the disk is not degrading;
  it is simply busy for a growing fraction of each second.
- Disk write latency is also constant at 0.919–0.930 ms/write.
- At spread-1200, 1,199.93 commits/s need 986 fsyncs/s (group commit already merges ~18 %), and
  the flush duty cycle is ~93 %.
- This wall is hidden behind the row-lock wall in the single-product runs, where throughput never
  gets high enough to stress fsync duty.

### 3. Pool of 6 connections amplifies the collapse (contributing, not root cause)

`DB_MAX_OPEN_CONNS=6` is fully checked out from 500 RPS upward in every run, including the clean
spread runs. When the row is contended, all six connections block on the same lock, so the
`database/sql` queue (unmeasured) adds to the wait and the 3 s timeout turns into a 503 storm.
Raising the pool alone would only create more waiters on the same row; the clean spread runs show
that 6 connections are enough for ≥1,200 RPS when rows do not collide.

Evidence: 6/6 backends at every rate ≥500; `pg_active` reaches 6 and lock waiters 5 only in the
single-product collapse; spread-1200 achieves 1,199.93/s with the same pool of 6.

### 4. API or PostgreSQL CPU saturation — rejected

API CPU peaks at 24 % of one core, PostgreSQL at 41 %, k6 at 31 %. `/health` previously sustained
~17.5k RPS on this box with p95 17 ms, and the health probe stayed under 11 ms during every burst.
Memory is not a factor (API RSS ≤68 MB, host free ≥2.7 GB).

### 5. Load-generator saturation — rejected as the cause of the 1,000 RPS failure

The generator is on the shared host, so it is a real caveat, but it is not what fails here:

- single-1000 drops 944 iterations while k6 uses only 29.5 % CPU and has 2,000 max VUs; the drop
  happens because each request occupies a VU for up to 3 s.
- spread-1200 runs 1,199.93/s with **0 dropped** and k6 CPU 30.7 %, so this generator can drive
  more than 1,200 req/s on this box.
- Errors are server 503s, not connection-refused or client timeouts.

### 6. Index maintenance / WAL volume — minor

WAL volume is only 0.21–0.46 MB/s and PostgreSQL CPU stays below 41 %. The `orders_product_id_idx`
adds a small amount of WAL per insert but cannot explain a 70× p50 jump at a 12.5 % rate increase.

## 7. Recommended next change

**Add group commit: batch order writes inside the API so one transaction and one WAL fsync cover
many orders.** Keep the public `POST /orders` contract unchanged (one product, one quantity per
request). A small in-process writer can collect pending orders for a 2–5 ms window (or 50–100
orders), then in one transaction:

1. `SELECT stock FROM products WHERE id = $1 FOR UPDATE` once,
2. check and decrement stock per order in that transaction,
3. `INSERT INTO orders (...) VALUES (...), (...), ...` for the accepted orders,
4. commit once.

This removes the per-order row-lock turn and the per-order fsync at the same time; a batch of 50
turns ~50 commits and ~50 fsyncs into one, while still returning 201/409 per request and preserving
`fsync=on` + `synchronous_commit=on`.

**Metric that proves it helped:** `pg_stat_wal.wal_sync` delta ÷ committed orders (fsyncs per
order). Today it is ≈1.0 in the clean runs (e.g. spread-1200: 986 fsync/s for 1,200 commits/s).
With a batch of 50 it should fall to ≤0.02, and a **single-product 1,000 RPS run should hold
p95 < 100 ms with 0 × 503** instead of the current p95 3.0 s / 5,031 × 503. The same run also
re-checks stock reconciliation and the 100-unit burst.

For isolation only, a temporary `synchronous_commit=off` run would confirm hypothesis 2 quickly,
but it must not be the shipped change: it breaks the commit durability requirement.

## 8. Missing measurements and open items

| Item | Status | What is needed |
|---|---|---|
| Connection-pool wait (`sql.DBStats.WaitCount/WaitDuration`) | **Missing** — inferred from 6/6 `apitest` backends | Export `db.Stats()` via expvar/metrics (or log it after each test) and rerun `run_case.sh`; no config change is required |
| CPU/RAM/fsync for bursts | **Missing** — sampler was not run for burst cases | Wrap `run_burst.sh` with `python3 sample.py --out results/<label>/samples.csv --duration 25`; the script is already present |
| Exact judge deadline | **Unknown** | Report uses 100 ms / 500 ms / 1 s; if a deadline is provided, the k6 summary histograms or a rerun with new thresholds give the exact pass count |
| Separate load generator | **Not available** | All numbers are shared-host. A same-region t3.small running `writes-fixed.js` with `BASE_URL=http://<deployed-host>:8080` would remove the k6 CPU competition |
| Lock waits below 1 s | **Under-sampled** — 1 Hz snapshots | `pg_stat_activity` sampling cannot see sub-second waits; the 900/1000 runs still show the waiters consistently |
| Requests committed but answered 503 | **Observed, explained** | 11 and 6 orders at single-1000 committed after the 3 s client-side deadline; count committed orders from the table, not from 201s |
| Autovacuum noise from per-run fixture deletes | **Limitation** | `setup-fixtures.sql` deletes the previous run's test orders; autovacuum may reclaim dead tuples during a run. It affects every run but is not separately measured. Add `VACUUM (ANALYZE) orders` after the delete for future runs |

## 9. Raw results

Everything is committed under `loadtest/diagnostics/results/`:

- `results.csv` — one row per run with every field used in this report.
- `env.txt` — captured OS, service unit, PostgreSQL settings, schema/indexes, row counts (password redacted).
- `results/<label>/summary.json` — full k6 summary (status counters, latency percentiles, durations).
- `results/<label>/samples.csv` — 1 Hz API/PG/k6 CPU, RSS, pool/active sessions, lock waiters, WAL and disk counters.
- `results/<label>/before.txt`, `after.txt` — order counts and stock for reconciliation.
- `results/<label>/meta.txt` — target rate, products, config, binary hash, timestamps, exit code.
- `results/<label>/k6.log`, `warmup-summary.json` — raw console output and warm-up summary.
- `results/burst-*/recovery.csv` — 10 Hz `/health` latency around each burst.

Harness source: `loadtest/diagnostics/{run_case.sh,run_burst.sh,sample.py,analyze.py,env.sh,matrix.sh,setup-fixtures.sql,snapshot.sql}`;
load scripts: `loadtest/writes-fixed.js` and `loadtest/burst.js`.

## 10. What this means for the targets

- **5,000 orders/s** is not reachable with one row per product and one fsync per order on this
  box. With the current code the single-product ceiling is ~900 committed/s and the fsync ceiling
  is ~1,000–1,100/s per node. Reaching 5,000 needs batching/group commit (recommended above),
  and/or sharding the hot product's stock across several rows plus multiple nodes.
- **100,000 orders/s** needs an asynchronous ingest path (queue + batched inserts), not a
  synchronous single-row commit per request. At the measured 0.94 ms per durable fsync, even
  perfect batching on one node cannot reach six figures; this is a multi-node + batch/queue
  design.
- **100,000 reads/s** is a different path and is not addressed by these write runs; the earlier
  read/health baselines are in `loadtest/CAPACITY.md` and remain shared-host measurements.
