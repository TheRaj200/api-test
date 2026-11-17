# Deploy on Ubuntu EC2

These steps run the Go API and PostgreSQL on the same instance. They use a
dedicated PostgreSQL role and a separate Linux service account, both named
`apitest`. Keep PostgreSQL on loopback. In the EC2 security group, allow TCP
8080 from the load generator and TCP 22 from your administration IP.

## 1. Set up the database

From the repository root, with Go 1.22+ available:

```bash
sudo apt update
sudo apt install -y postgresql
sudo systemctl enable --now postgresql
sudo -u postgres createuser --pwprompt apitest
sudo -u postgres createdb -O apitest api_test
psql -h 127.0.0.1 -U apitest -d api_test -W -v ON_ERROR_STOP=1 -f db/schema.sql
```

Create the role and database once per instance. The schema seeds product 1
with 100 units on a fresh database.

## 2. Install the binary and service

```bash
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -o bin/api-test ./cmd/api
id -u apitest >/dev/null 2>&1 || sudo useradd --system --user-group --no-create-home apitest
sudo install -d -m 0755 /opt/api-test
sudo install -m 0755 bin/api-test /opt/api-test/api-test
sudo install -d -m 0700 /etc/api-test
sudo test -f /etc/api-test/api-test.env || sudo install -m 0600 /dev/null /etc/api-test/api-test.env
sudoedit /etc/api-test/api-test.env
```

Create the environment file once. For subsequent deployments, preserve it.
Add this line using the password from step 1. Percent-encode any URL-reserved
characters in the password.

```ini
DATABASE_URL=postgres://apitest:REPLACE_WITH_URL_ENCODED_PASSWORD@127.0.0.1:5432/api_test?sslmode=disable
```

```bash
sudo install -m 0644 deploy/api-test.service /etc/systemd/system/api-test.service
sudo systemctl daemon-reload
sudo systemctl enable --now api-test
sudo systemctl status api-test --no-pager
```

When updating the binary of an already running service, run
`sudo systemctl restart api-test` after installation. For startup errors,
run `sudo journalctl -u api-test -n 50 --no-pager`.

## 3. Smoke test and benchmark

```bash
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS -X POST http://127.0.0.1:8080/orders \
  -H 'Content-Type: application/json' \
  -d '{"product_id":1,"quantity":1}'
# Replace 1 with the ID returned by POST.
curl -fsS http://127.0.0.1:8080/orders/1
```

Check `/readyz` from the load generator too. Reset the dedicated practice
database before the 100-unit burst test because the smoke order uses one
unit. See [load test instructions](../loadtest/README.md).

For a 2 vCPU / 4 GB instance, start with PostgreSQL defaults and the unit's
six database connections. Change one setting at a time and rerun the same
workload. During a sustained write test, measure successful orders while
stock is available. During a sold-out burst, count expected out-of-stock
responses separately from timeouts and server errors. Keep PostgreSQL's
default synchronous commit behavior so acknowledged orders are durable.

The checked-in unit also sets `GOMEMLIMIT=128MiB` (runtime memory target),
`MemoryMax=256M` (service limit), and `LimitNOFILE=4096`. Review observed
memory and open connections during the rehearsal before changing these
limits. The optional `postgresql-1gb.conf` is for a 1 GB instance.
