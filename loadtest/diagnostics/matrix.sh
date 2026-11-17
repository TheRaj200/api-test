#!/usr/bin/env bash
# Full measured matrix. Run from /tmp/diag: nohup bash matrix.sh > matrix.log 2>&1 &
set -euo pipefail
cd /tmp/diag
rm -rf results/single-500-validate
rm -f matrix.done matrix.failed

{
  echo "matrix_start=$(date -Is)"
  for r in 500 650 800 1000; do
    bash run_case.sh single-$r $r 9001 60s 10s
  done
  for r in 500 650 800 1000; do
    bash run_case.sh spread-$r $r 9001,9002,9003,9004 60s 10s
  done
  for r in 650 800 1000; do
    bash run_case.sh single-$r-b $r 9001 60s 10s
  done
  for r in 650 800 1000; do
    bash run_case.sh spread-$r-b $r 9001,9002,9003,9004 60s 10s
  done
  echo "matrix_end=$(date -Is)"
} 2>&1 | tee matrix.log
echo "ok" >matrix.done
