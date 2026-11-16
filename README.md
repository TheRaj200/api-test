# api-test

[![ci](https://github.com/TheRaj200/api-test/actions/workflows/ci.yml/badge.svg)](https://github.com/TheRaj200/api-test/actions/workflows/ci.yml)

A small order API in Go + PostgreSQL, with unit and integration tests, k6 load
tests, and a deploy recipe for 1 vCPU / 1 GB.

## Layout

```
cmd/api/          server entrypoint (pool, timeouts, graceful shutdown)
internal/api/     HTTP handlers, JSON helpers, handler tests
internal/store/   SQL queries, integration + race tests
internal/config/  environment configuration
db/schema.sql     tables, index and seed row
loadtest/         k6 scripts and measured results
deploy/           systemd unit and Postgres settings for a 1 GB box
```

Requires Go 1.22+, PostgreSQL, and psql. Use a dedicated local database named
`api_test`. Set the URL below for your own local account.

```powershell
$env:DATABASE_URL = "postgres://postgres:YOUR_LOCAL_PASSWORD@localhost:5432/api_test?sslmode=disable"
psql $env:DATABASE_URL -v ON_ERROR_STOP=1 -f db/schema.sql
go mod tidy
go run ./cmd/api
```

`make run`, `make build` (static binary in `bin/`) and `make check` (gofmt, vet,
tests) wrap the same commands.

Set `PORT` to listen somewhere other than `:8080`, and `DB_MAX_OPEN_CONNS` to
size the Postgres pool (default 8). Load tests live in `loadtest/`
(`loadtest/README.md`, results in `loadtest/RESULTS.md`). Deployment on a small
VPS is covered by `deploy/README.md`.

## Endpoints

| Method | Path | Responses |
|---|---|---|
| `GET` | `/health` | `200` while the process runs; no database call |
| `GET` | `/readyz` | `200` when PostgreSQL answers, `503` when it does not |
| `POST` | `/orders` | `201` created, `400` invalid body, `409` no stock |
| `GET` | `/orders/{id}` | `200`, `400` bad id, `404` missing |

`/health` is for liveness, `/readyz` is what a load balancer or deploy script
should poll before sending traffic.

## Tests

```powershell
go test ./...                 # handlers and config, no database needed
$env:TEST_DATABASE_URL = "postgres://postgres:PASSWORD@localhost:5432/api_test?sslmode=disable"
go test -count=1 ./...        # adds store integration + concurrent stock tests
```

The store tests insert a random product, drive it through the same CTE as the
API, and assert that 50 concurrent orders against 10 units produce exactly 10
successes. They skip cleanly when `TEST_DATABASE_URL` is unset, which is how CI
runs the unit job.

In a second PowerShell window:


```powershell
Invoke-RestMethod http://localhost:8080/health
$order = Invoke-RestMethod http://localhost:8080/orders -Method Post -ContentType 'application/json' -Body '{"product_id":1,"quantity":1}'
Invoke-RestMethod "http://localhost:8080/orders/$($order.id)"
```

For a clean concurrency rehearsal, use a fresh database seeded by db/schema.sql.
Send 500 concurrent POST requests for product 1, quantity 1. Expect 100 HTTP 201
responses, 400 HTTP 409 responses, 100 stored orders, and zero stock.
Check the database with:

```sql
SELECT stock FROM products WHERE id = 1;
SELECT count(*) FROM orders WHERE product_id = 1;
```

This practice API has no idempotency contract. Each successful POST creates
a separate order. The connection string example is for local use.
