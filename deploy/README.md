# Deploying on 1 vCPU / 1 GB RAM

Target: a small Ubuntu/Debian VPS. Expected footprint: Postgres ~300 MB,
Go process ~20-30 MB, rest left to the OS.

## 1. Postgres

```bash
sudo apt install -y postgresql
# append the tuned settings
sudo tee -a /etc/postgresql/*/main/conf.d/10-small.conf \
  < deploy/postgresql-1gb.conf
sudo systemctl restart postgresql

sudo -u postgres createuser --pwprompt apitest
sudo -u postgres createdb -O apitest api_test
```

Apply the schema with either a password prompt or a `.pgpass` file:

```bash
PGPASSWORD=... psql "postgres://apitest@localhost:5432/api_test" -f db/schema.sql
```

`max_connections = 20` and `DB_MAX_OPEN_CONNS = 6` together keep the backend
process count (and therefore RAM) bounded.

## 2. Binary

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o api-test ./cmd/api
sudo install -d -o apitest -g apitest /opt/api-test
sudo install -o apitest -g apitest api-test /opt/api-test/api-test
```

The stripped static binary is ~8 MB and idles around 15 MB RSS.

## 3. systemd

```bash
sudo cp deploy/api-test.service /etc/systemd/system/api-test.service
sudo nano /etc/systemd/system/api-test.service   # set the DB password
sudo systemctl enable --now api-test
systemctl status api-test --no-pager
journalctl -u api-test -f
```

## 4. Smoke test

```bash
curl -s localhost:8080/health
curl -s -X POST localhost:8080/orders \
  -H 'Content-Type: application/json' \
  -d '{"product_id":1,"quantity":1}'
```

## Capacity expectations on this size

Measured on 12 cores / 15 GB, so treat these as an upper bound; a 1 vCPU box
lands lower, roughly proportional to core count for the write path:

| Workload | 12-core result | 1 vCPU estimate |
|---|---|---|
| `GET /health` | ~40k req/s | ~4-6k req/s |
| Mixed reads with DB | 10k req/s, p95 < 1 ms | ~3-5k req/s |
| `POST /orders` | ~1,500/s ceiling | ~300-600/s |

The write path is bound by one WAL fsync per order, which is per-commit work on
a single core.

## If writes need to go faster

1. `synchronous_commit = off` in `postgresql.conf` — biggest single win, loses
   the last few hundred ms of orders on a crash.
2. Batch inserts: accept N orders per request or per queued tick, so one
   COMMIT covers many rows.
3. Put the database on separate storage/instance and let the API stay tiny.

## Tuning knobs exposed by the app

| Env var | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | required | Postgres DSN |
| `PORT` | `8080` | listen port |
| `DB_MAX_OPEN_CONNS` | `8` | pool size (max idle tracks it) |
| `GOMEMLIMIT` | none | Go runtime soft memory cap, set `128MiB` |
