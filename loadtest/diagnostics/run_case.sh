#!/usr/bin/env bash
# One measured write case: 10 s warm-up, 60 s measured, DB + resource snapshots.
#
#   bash run_case.sh single-500 500 9001
#   bash run_case.sh spread-500 500 9001,9002,9003,9004
#
# Assumes the harness lives in /tmp/diag and fixtures in setup-fixtures.sql.
set -euo pipefail

LABEL=${1:?label}
RATE=${2:?target rate}
PRODUCTS=${3:?product ids}
DUR=${4:-60s}
WARMUP=${5:-10s}
BASE=${6:-http://127.0.0.1:8080}

DIAG=/tmp/diag
OUT=$DIAG/results/$LABEL
DUR_SEC=${DUR%s}
WARM_SEC=${WARMUP%s}

rm -rf "$OUT"
mkdir -p "$OUT"
ulimit -n 65535 2>/dev/null || true

sql() { sudo -n -u postgres psql -tA -d api_test -c "$1"; }

reset_fixtures() {
  sudo -n -u postgres psql -q -d api_test -f "$DIAG/setup-fixtures.sql" >/dev/null
}

{
  echo "label=$LABEL"
  echo "target_rate=$RATE"
  echo "products=$PRODUCTS"
  echo "duration=$DUR"
  echo "warmup=$WARMUP"
  echo "base_url=$BASE"
  echo "started_at=$(date -Is)"
  echo "host=$(hostname)"
  echo "kernel=$(uname -r)"
  echo "cpus=$(nproc)"
  echo "k6_version=$(k6 version | head -1)"
  echo "api_binary_sha256=$(sha256sum /opt/api-test/api-test | cut -d' ' -f1)"
  echo "max_open_conns=$(systemctl show api-test -p Environment --value | tr ' ' '\n' | grep DB_MAX_OPEN_CONNS || true)"
  echo "synchronous_commit=$(sql 'SHOW synchronous_commit')"
  echo "track_wal_io_timing=$(sql 'SHOW track_wal_io_timing')"
  echo "shared_buffers=$(sql 'SHOW shared_buffers')"
  echo "max_connections=$(sql 'SHOW max_connections')"
  echo "wal_level=$(sql 'SHOW wal_level')"
} >"$OUT/meta.txt"

reset_fixtures

# Warm up the same path at the same rate so caches and pools are steady.
BASE_URL=$BASE k6 run --quiet --no-color \
  -e RATE=$RATE -e DUR=$WARMUP -e PRODUCT_IDS=$PRODUCTS \
  -e SUMMARY_PATH=$OUT/warmup-summary.json \
  "$DIAG/lt/writes-fixed.js" >"$OUT/warmup-k6.log" 2>&1

sudo -n -u postgres psql -tA -F, -d api_test -f "$DIAG/snapshot.sql" >"$OUT/before.txt"

python3 "$DIAG/sample.py" --out "$OUT/samples.csv" --duration $((DUR_SEC + 4)) --interval 1 &
SAMPLER=$!

BASE_URL=$BASE k6 run --quiet --no-color \
  -e RATE=$RATE -e DUR=$DUR -e PRODUCT_IDS=$PRODUCTS \
  -e SUMMARY_PATH=$OUT/summary.json \
  "$DIAG/lt/writes-fixed.js" >"$OUT/k6.log" 2>&1
K6_EXIT=$?

wait $SAMPLER 2>/dev/null || true

sudo -n -u postgres psql -tA -F, -d api_test -f "$DIAG/snapshot.sql" >"$OUT/after.txt"
echo "finished_at=$(date -Is)" >>"$OUT/meta.txt"
echo "k6_exit=$K6_EXIT" >>"$OUT/meta.txt"

echo "done $LABEL"
