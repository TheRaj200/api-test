# Write-path diagnostics

Harness used to produce `loadtest/DIAGNOSTIC_REPORT.md`. It runs on the deployed
instance (`/tmp/diag`) because no separate load-generator host was available, so
every result is a **shared-host** measurement: k6 competes with the API and
PostgreSQL for the same 2 vCPU.

## Layout on the instance

```
/tmp/diag/
  env.sh                 # one-off config snapshot (password redacted)
  setup-fixtures.sql     # isolated products 9001-9004 (5M stock), 9201 (100)
  snapshot.sql           # order counts + stock before/after each run
  sample.py              # 1 s CPU/RAM/lock/WAL/disk sampler
  run_case.sh            # warm-up + 60 s measured run
  run_burst.sh           # burst + latency-recovery probe
  analyze.py             # results.csv + report tables
  lt/writes-fixed.js     # fixed-rate writes, one or many products
  lt/burst.js            # shared-iterations burst
  results/<label>/       # summary.json, k6.log, samples.csv, before/after
```

## Commands

```bash
# 0. upload harness + scripts, then capture environment
bash /tmp/diag/env.sh | tee /tmp/diag/results/env.txt

# 1. single product (9001)
for r in 500 650 800 1000; do bash /tmp/diag/run_case.sh single-$r $r 9001; done

# 2. same load spread across four products
for r in 500 650 800 1000; do bash /tmp/diag/run_case.sh spread-$r $r 9001,9002,9003,9004; done

# 3. repeat the steps around the knee
for r in 650 800 1000; do bash /tmp/diag/run_case.sh single-$r-b $r 9001; done
for r in 650 800 1000; do bash /tmp/diag/run_case.sh spread-$r-b $r 9001,9002,9003,9004; done

# 4. bursts on exactly 100 units
bash /tmp/diag/run_burst.sh burst-500 500 100
bash /tmp/diag/run_burst.sh burst-1000 1000 200
bash /tmp/diag/run_burst.sh burst-2000 2000 400
bash /tmp/diag/run_burst.sh burst-4000 4000 800

# 5. aggregate
python3 /tmp/diag/analyze.py /tmp/diag/results
```

Each `run_case.sh` resets the isolated products first, so stock is fresh for
every run. `synchronous_commit` and every other database setting stay at the
deployed defaults; the harness never changes them.

## What is measured directly vs inferred

- Direct: response status mix, latency percentiles, started/completed/dropped
  iterations, DB order-count delta, stock delta, `/proc` CPU and RSS for the API,
  PostgreSQL and k6, WAL `wal_sync`/`wal_sync_time`, `/proc/diskstats` write
  completion time, `pg_stat_activity` sessions and lock waiters.
- Inferred: connection-pool wait. The Go pool stats are not exported, so the
  number of `apitest` client backends is compared against
  `DB_MAX_OPEN_CONNS=6`; a value of 6 means the pool is fully checked out and
  requests are queueing in `database/sql`.
