#!/usr/bin/env bash
# One burst case: exactly 100 units on product 9201, N requests of quantity 1.
# A 4 req/s /health probe runs across the whole burst so the report can show
# when latency returns to baseline after the burst.
#
#   bash run_burst.sh burst-500 500 100
set -euo pipefail

LABEL=${1:?label}
REQUESTS=${2:?requests}
VUS=${3:-100}
MAXD=${4:-120s}
BASE=${5:-http://127.0.0.1:8080}

DIAG=/tmp/diag
OUT=$DIAG/results/$LABEL
rm -rf "$OUT"
mkdir -p "$OUT"
ulimit -n 65535 2>/dev/null || true

sql() { sudo -n -u postgres psql -tA -d api_test -c "$1"; }

sudo -n -u postgres psql -q -d api_test -f "$DIAG/setup-fixtures.sql" >/dev/null

{
  echo "label=$LABEL"
  echo "target_rate=burst"
  echo "products=9201"
  echo "requests=$REQUESTS"
  echo "vus=$VUS"
  echo "max_duration=$MAXD"
  echo "base_url=$BASE"
  echo "started_at=$(date -Is)"
  echo "host=$(hostname)"
  echo "cpus=$(nproc)"
  echo "k6_version=$(k6 version | head -1)"
  echo "api_binary_sha256=$(sha256sum /opt/api-test/api-test | cut -d' ' -f1)"
  echo "synchronous_commit=$(sql 'SHOW synchronous_commit')"
  echo "track_wal_io_timing=$(sql 'SHOW track_wal_io_timing')"
} >"$OUT/meta.txt"

sudo -n -u postgres psql -tA -F, -d api_test -f "$DIAG/snapshot.sql" >"$OUT/before.txt"

PROBE_START=$(date +%s%3N)
: >"$OUT/recovery.csv"
(
  for i in $(seq 1 200); do
    t=$(date +%s%3N)
    h=$(curl -s -o /dev/null -m 5 -w '%{http_code},%{time_total}' http://127.0.0.1:8080/health || true)
    echo "$((t - PROBE_START)),health,$h" >>"$OUT/recovery.csv"
    sleep 0.1
  done
) &
PROBER=$!

BASE_URL=$BASE k6 run --quiet --no-color \
  -e REQUESTS=$REQUESTS -e VUS=$VUS -e PRODUCT_ID=9201 -e MAX_DURATION=$MAXD \
  -e SUMMARY_PATH=$OUT/summary.json \
  "$DIAG/lt/burst.js" >"$OUT/k6.log" 2>&1
K6_EXIT=$?
echo "burst_finished_ms=$(( $(date +%s%3N) - PROBE_START ))" >>"$OUT/meta.txt"

wait $PROBER 2>/dev/null || true

sudo -n -u postgres psql -tA -F, -d api_test -f "$DIAG/snapshot.sql" >"$OUT/after.txt"
echo "finished_at=$(date -Is)" >>"$OUT/meta.txt"
echo "k6_exit=$K6_EXIT" >>"$OUT/meta.txt"

echo "done $LABEL"
