#!/usr/bin/env python3
"""Sample API/PostgreSQL CPU, memory, lock and storage counters during a run.

Writes one CSV row per interval so the report can separate API saturation from
load-generator (k6) saturation on the shared host, and can show WAL fsync and
disk write latency while the write path is saturated.

Usage: sample.py --out samples.csv --duration 63 --interval 1
"""
import argparse
import csv
import os
import subprocess
import time

HZ = os.sysconf("SC_CLK_TCK")
DEVICE = os.environ.get("DIAG_DEVICE", "nvme0n1")

PG_QUERY = (
    "SELECT "
    "(SELECT count(*) FROM pg_stat_activity WHERE backend_type='client backend' AND usename='apitest'),"
    "(SELECT count(*) FROM pg_stat_activity WHERE backend_type='client backend' AND usename='apitest' AND state='active'),"
    "(SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock'),"
    "(SELECT count(*) FROM pg_locks WHERE NOT granted),"
    "(SELECT coalesce(sum(xact_commit),0) FROM pg_stat_database WHERE datname='api_test'),"
    "(SELECT coalesce(sum(xact_rollback),0) FROM pg_stat_database WHERE datname='api_test'),"
    "(SELECT coalesce(sum(blks_hit),0) FROM pg_stat_database WHERE datname='api_test'),"
    "(SELECT coalesce(sum(blks_read),0) FROM pg_stat_database WHERE datname='api_test'),"
    "(SELECT coalesce(sum(wal_records),0) FROM pg_stat_wal),"
    "(SELECT coalesce(sum(wal_fpi),0) FROM pg_stat_wal),"
    "(SELECT coalesce(sum(wal_bytes),0) FROM pg_stat_wal),"
    "(SELECT coalesce(sum(wal_write),0) FROM pg_stat_wal),"
    "(SELECT coalesce(sum(wal_sync),0) FROM pg_stat_wal),"
    "(SELECT coalesce(sum(wal_write_time),0) FROM pg_stat_wal),"
    "(SELECT coalesce(sum(wal_sync_time),0) FROM pg_stat_wal)"
)

PG_FIELDS = [
    "pg_conns",
    "pg_active",
    "pg_lock_wait_events",
    "pg_locks_not_granted",
    "xact_commit",
    "xact_rollback",
    "blks_hit",
    "blks_read",
    "wal_records",
    "wal_fpi",
    "wal_bytes",
    "wal_write",
    "wal_sync",
    "wal_write_time",
    "wal_sync_time",
]

HEADER = [
    "t_rel_ms",
    "api_cpu_pct",
    "api_rss_mb",
    "pg_cpu_pct",
    "pg_rss_mb",
    "k6_cpu_pct",
    "load1",
    "mem_avail_mb",
    "disk_writes",
    "disk_write_ms",
] + PG_FIELDS


def pids(name):
    try:
        out = subprocess.run(
            ["pgrep", "-x", name], capture_output=True, text=True, timeout=3
        ).stdout.split()
        return [int(p) for p in out]
    except Exception:
        return []


def ticks(pid):
    try:
        with open("/proc/%d/stat" % pid) as fh:
            parts = fh.read().split()
    except OSError:
        return 0
    if len(parts) < 15:
        return 0
    return int(parts[13]) + int(parts[14])


def rss_kb(pid):
    try:
        with open("/proc/%d/status" % pid) as fh:
            for line in fh:
                if line.startswith("VmRSS:"):
                    return int(line.split()[1])
    except OSError:
        pass
    return 0


def cpu_snapshot(name, prev):
    cur = {p: ticks(p) for p in pids(name)}
    delta = 0
    for pid, now in cur.items():
        if pid in prev and now >= prev[pid]:
            delta += now - prev[pid]
    return delta, cur, sum(rss_kb(p) for p in cur)


def disk_snapshot():
    try:
        with open("/proc/diskstats") as fh:
            for line in fh:
                parts = line.split()
                if len(parts) > 10 and parts[2] == DEVICE:
                    return int(parts[7]), int(parts[10])
    except OSError:
        pass
    return 0, 0


def load1():
    try:
        with open("/proc/loadavg") as fh:
            return float(fh.read().split()[0])
    except OSError:
        return 0.0


def mem_avail_mb():
    try:
        with open("/proc/meminfo") as fh:
            for line in fh:
                if line.startswith("MemAvailable:"):
                    return int(line.split()[1]) // 1024
    except OSError:
        pass
    return 0


def pg_snapshot():
    try:
        out = subprocess.run(
            ["sudo", "-n", "-u", "postgres", "psql", "-tA", "-F", "|", "-d", "api_test", "-c", PG_QUERY],
            capture_output=True,
            text=True,
            timeout=5,
        )
        row = out.stdout.strip().split("|")
        if len(row) != len(PG_FIELDS):
            return {f: "" for f in PG_FIELDS}
        # Some sums (WAL timing columns) come back as fractional numerics.
        return {f: float(v or 0) for f, v in zip(PG_FIELDS, row)}
    except Exception:
        return {f: "" for f in PG_FIELDS}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--duration", type=float, default=63)
    ap.add_argument("--interval", type=float, default=1)
    args = ap.parse_args()

    start = time.time()
    end = start + args.duration
    prev_api, prev_pg, prev_k6 = {}, {}, {}
    prev_time = None

    def pct(delta):
        # Tick deltas cover the gap since the previous sample, so divide by that
        # gap (not by the short time spent reading /proc).
        if not prev_time or delta <= 0:
            return 0.0
        return round(delta / HZ / (tick_start - prev_time) * 100, 2)

    with open(args.out, "w", newline="") as fh:
        writer = csv.writer(fh)
        writer.writerow(HEADER)
        while time.time() < end:
            tick_start = time.time()
            api_d, prev_api, api_rss = cpu_snapshot("api-test", prev_api)
            pg_d, prev_pg, pg_rss = cpu_snapshot("postgres", prev_pg)
            k6_d, prev_k6, _ = cpu_snapshot("k6", prev_k6)
            cur_disk = disk_snapshot()
            pg = pg_snapshot()

            row = {
                "t_rel_ms": int((tick_start - start) * 1000),
                "api_cpu_pct": pct(api_d),
                "api_rss_mb": round(api_rss / 1024, 1),
                "pg_cpu_pct": pct(pg_d),
                "pg_rss_mb": round(pg_rss / 1024, 1),
                "k6_cpu_pct": pct(k6_d),
                "load1": round(load1(), 2),
                "mem_avail_mb": mem_avail_mb(),
                "disk_writes": cur_disk[0],
                "disk_write_ms": cur_disk[1],
            }
            row.update(pg)
            writer.writerow([row[c] for c in HEADER])
            prev_time = tick_start

            sleep_for = args.interval - (time.time() - tick_start)
            if sleep_for > 0:
                time.sleep(sleep_for)


if __name__ == "__main__":
    main()
