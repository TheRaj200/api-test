# api-test

A small order API in Go + PostgreSQL, with k6 load tests and a deploy recipe for
1 vCPU / 1 GB.

## Layout

```
cmd/api/          server entrypoint (flags, pool, graceful shutdown)
internal/api/     HTTP handlers and JSON helpers
internal/store/   SQL queries
internal/config/  environment configuration
db/schema.sql     tables and seed row
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

Set `PORT` to listen somewhere other than `:8080`, and `DB_MAX_OPEN_CONNS` to
size the Postgres pool (default 8). Load tests live in `loadtest/`
(`loadtest/README.md`, results in `loadtest/RESULTS.md`). Deployment on a small VPS is
covered by `deploy/README.md`.

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
