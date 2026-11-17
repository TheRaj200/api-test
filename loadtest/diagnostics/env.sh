#!/usr/bin/env bash
# Capture the deployed configuration once, with the DB password redacted.
set -euo pipefail

sql() { sudo -n -u postgres psql -tA -d api_test -c "$1"; }

echo "captured_at=$(date -Is)"
echo "host=$(hostname)"
echo "os=$(grep PRETTY /etc/os-release | cut -d= -f2- | tr -d '\"')"
echo "kernel=$(uname -r)"
echo "arch=$(uname -m)"
echo "cpus=$(nproc)"
echo "mem_total_mb=$(free -m | sed -n 2p | awk '{print $2}')"
echo "disk=$(df -h / | tail -1 | awk '{print $2" total, "$4" free"}')"
echo "selinux=$(getenforce 2>/dev/null || echo unknown)"
echo "ulimit_nofile=$(ulimit -n)"
echo "python3=$(python3 --version 2>&1)"
echo "k6_version=$(k6 version | head -1)"
echo "api_binary_sha256=$(sha256sum /opt/api-test/api-test | cut -d' ' -f1)"
echo
echo "===== systemd unit ====="
systemctl cat api-test --no-pager
echo
echo "===== runtime limits / env (password redacted) ====="
systemctl show api-test -p Environment -p MemoryMax -p LimitNOFILE -p Restart -p ActiveState --no-pager
sudo sed -E 's#(://[^:]+:)[^@]+@#\1REDACTED@#g' /etc/api-test/api-test.env
echo
echo "===== postgres version ====="
sql 'SELECT version()'
echo
echo "===== postgres settings ====="
sql "SELECT name || '=' || setting FROM pg_settings WHERE name IN ('shared_buffers','max_connections','synchronous_commit','fsync','full_page_writes','wal_level','work_mem','maintenance_work_mem','max_wal_size','min_wal_size','checkpoint_timeout','checkpoint_completion_target','effective_cache_size','random_page_cost','max_worker_processes') ORDER BY name"
echo
echo "===== pg_stat_wal columns ====="
sql "SELECT string_agg(column_name, ', ' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_name='pg_stat_wal'"
echo
echo "===== schema ====="
sql "SELECT table_name || '.' || column_name || ' ' || data_type FROM information_schema.columns WHERE table_schema='public' ORDER BY table_name, ordinal_position"
echo
echo "===== indexes ====="
sql "SELECT indexname || ' :: ' || indexdef FROM pg_indexes WHERE schemaname='public' ORDER BY indexname"
echo
echo "===== row counts ====="
sql "SELECT 'products=' || count(*) FROM products"
sql "SELECT 'orders=' || count(*) FROM orders"
echo
echo "===== database size ====="
sql "SELECT pg_size_pretty(pg_database_size('api_test'))"
