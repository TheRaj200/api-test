# api-test

Requires Go 1.22+, PostgreSQL, and psql. Use a dedicated local database named
`api_test`. Set the URL below for your own local account.

```powershell
$env:DATABASE_URL = "postgres://postgres:YOUR_LOCAL_PASSWORD@localhost:5432/api_test?sslmode=disable"
psql $env:DATABASE_URL -v ON_ERROR_STOP=1 -f schema.sql
go mod tidy
go run .
```

In a second PowerShell window:

```powershell
Invoke-RestMethod http://localhost:8080/health
$order = Invoke-RestMethod http://localhost:8080/orders -Method Post -ContentType 'application/json' -Body '{"product_id":1,"quantity":1}'
Invoke-RestMethod "http://localhost:8080/orders/$($order.id)"
```

For a clean concurrency rehearsal, use a fresh database seeded by schema.sql.
Send 500 concurrent POST requests for product 1, quantity 1. Expect 100 HTTP 201
responses, 400 HTTP 409 responses, 100 stored orders, and zero stock.
Check the database with:

```sql
SELECT stock FROM products WHERE id = 1;
SELECT count(*) FROM orders WHERE product_id = 1;
```

This practice API has no idempotency contract. Each successful POST creates
a separate order. The connection string example is for local use.
