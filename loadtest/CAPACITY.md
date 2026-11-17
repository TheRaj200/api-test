# Capacity baseline and path to 100k req/s

Measured 2026-11-17 on the deployed EC2 instance:

- Amazon Linux 2023, **2 vCPU / 4 GB RAM**, 8 GB disk, ap-southeast-2
- Go API + PostgreSQL 16 on the same box, `DB_MAX_OPEN_CONNS=6`
- k6 **also on the same box**, so these numbers are conservative: the load
  generator steals roughly one of the two cores. A separate client in the same
  region would measure slightly higher.

## Measured ceilings (10 s runs, 0% server errors)

| Workload | Target | Achieved | p95 | Notes |
|---|---|---|---|---|
| `GET /health` | 10,000/s | **9,955/s** | 5.3 ms | 0 failed |
| `GET /health` | 20,000/s | **17,520/s** | 17.4 ms | k6 ran out of VUs, not the API |
| Mixed reads (50% DB) | 5,000/s | **5,000/s** | 4.3 ms | 0 failed |
| Mixed reads (50% DB) | 10,000/s | **9,123/s** | 37.7 ms | 0 failed |
| `POST /orders` | 500/s | **500/s** | 1.6 ms | 0 failed |
| `POST /orders` | 1,000/s | **886/s** | 1.06 s | 0.02% 5xx, queueing |
| `POST /orders` | 1,500/s | **900/s** | 2.21 s | 1.73% 5xx |

Single-box reality:

- **~18-20k req/s** for a no-database endpoint.
- **~9-10k req/s** for a read mix that hits PostgreSQL.
- **~900 orders/s** for a single product. The write-path diagnostic report shows
  this is first same-row lock serialization in `UPDATE products ...`; the
  per-order WAL fsync becomes the next wall (~1,000-1,100/s) once rows are
  spread. See `DIAGNOSTIC_REPORT.md`.

Server-side latency with no network in the way is 0.22 ms average for
`GET /health`, and the API process idles around 11 MB RSS.

## What 100k req/s actually requires

100k/s is **6x** the health ceiling, **11x** the read ceiling and **110x** the
order ceiling of this instance.

| Target | Realistic design |
|---|---|
| 100k `GET /health` per second | 6-8 app instances behind an ALB (stateless app, no DB call) |
| 100k mixed reads per second | 10-14 app instances + Redis cache in front of Postgres + read replicas |
| 100k orders per second | queue (SQS/Kafka) + batch inserts (500-1,000 rows per COMMIT); per-row fsync cannot get there |

Rough on-demand cost in ap-southeast-2 for the read-heavy case: 8 x t3.small
(~$120/mo) + ALB (~$20/mo) + ElastiCache micro (~$12/mo) + a dedicated
Postgres instance (~$30/mo) = **~$180-200/mo**, before data transfer. Spot or
reserved capacity cuts that roughly in half.

## Scaling order that keeps the tests honest

1. **Separate load generator** in the same region, so k6 does not compete with
   the API for CPU. Every number above moves up a little.
2. **Nginx/ALB in front of 2 instances** of the same binary. The app is already
   stateless: only `DATABASE_URL`, `PORT` and `DB_MAX_OPEN_CONNS` matter.
   Expect roughly linear scaling to ~35k health/s and ~18k mixed reads/s.
3. **Raise the write ceiling** before adding nodes: batch orders into one
   transaction, or move order creation to a queue. Nodes do not help a single
   WAL fsync.
4. **Cache reads** (Redis, 1-5 s TTL for product/order lookups) and add a read
   replica. This is what takes a read mix from ~10k/s per node toward 100k/s.
5. **Distributed load testing**: one k6 machine tops out around 50-80k req/s.
   Use 2-3 generators or k6 Cloud when pushing past that.

## Tools in this repo

- `rate-health.js` - pure `GET /health`, no database (new ceiling probe).
- `rate-read.js` - 50% health, 50% order lookup.
- `rate-write.js` - `POST /orders`, one synchronous commit per request.
- `concurrency.js` - the 100-of-500 oversell check.
- `RESULTS.md` - earlier before/after numbers on a 12-core dev machine.
