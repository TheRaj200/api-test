#!/usr/bin/env python3
"""Turn diagnostics/results/<label>/ into one CSV plus report-ready tables."""
import csv
import json
import os
import sys


def load_meta(path):
    meta = {}
    with open(path) as fh:
        for line in fh:
            if "=" in line:
                key, value = line.rstrip("\n").split("=", 1)
                meta[key] = value
    return meta


def load_snapshot(path):
    snap = {}
    with open(path) as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            key, value = line.rsplit(",", 1)
            snap[key] = int(value)
    return snap


def numeric(rows, column):
    out = []
    for row in rows:
        try:
            out.append(float(row[column]))
        except (KeyError, TypeError, ValueError):
            pass
    return out


def sampler_stats(path, window_ms=None):
    stats = {
        "api_cpu_avg": "MISSING",
        "api_cpu_max": "MISSING",
        "api_rss_mb": "MISSING",
        "pg_cpu_avg": "MISSING",
        "pg_cpu_max": "MISSING",
        "pg_rss_mb": "MISSING",
        "k6_cpu_avg": "MISSING",
        "load1_max": "MISSING",
        "mem_avail_min_mb": "MISSING",
        "pg_conns_max": "MISSING",
        "pg_active_max": "MISSING",
        "pg_lock_wait_max": "MISSING",
        "pg_locks_not_granted_max": "MISSING",
        "wal_sync_ms": "MISSING",
        "wal_write_ms": "MISSING",
        "disk_write_ms": "MISSING",
        "xact_per_sec": "MISSING",
        "wal_sync_per_sec": "MISSING",
        "wal_mb_per_sec": "MISSING",
        "disk_writes_per_sec": "MISSING",
        "samples": 0,
    }
    if not os.path.exists(path):
        return stats
    with open(path) as fh:
        rows = list(csv.DictReader(fh))
    if window_ms:
        active = [r for r in rows if float(r["t_rel_ms"]) <= window_ms]
        if len(active) >= 2:
            rows = active
    if not rows:
        return stats
    stats["samples"] = len(rows)

    for column, avg_key, max_key in [
        ("api_cpu_pct", "api_cpu_avg", "api_cpu_max"),
        ("pg_cpu_pct", "pg_cpu_avg", "pg_cpu_max"),
    ]:
        values = numeric(rows, column)
        if values:
            stats[avg_key] = round(sum(values) / len(values), 1)
            stats[max_key] = round(max(values), 1)

    for column, key, fn in [
        ("api_rss_mb", "api_rss_mb", max),
        ("pg_rss_mb", "pg_rss_mb", max),
        ("k6_cpu_pct", "k6_cpu_avg", lambda v: round(sum(v) / len(v), 1)),
        ("load1", "load1_max", max),
        ("mem_avail_mb", "mem_avail_min_mb", min),
        ("pg_conns", "pg_conns_max", max),
        ("pg_active", "pg_active_max", max),
        ("pg_lock_wait_events", "pg_lock_wait_max", max),
        ("pg_locks_not_granted", "pg_locks_not_granted_max", max),
    ]:
        values = numeric(rows, column)
        if values:
            stats[key] = round(fn(values), 1)

    wal = [(float(r["wal_sync"]), float(r["wal_sync_time"])) for r in rows
           if r.get("wal_sync") not in ("", None)]
    if len(wal) >= 2 and wal[-1][0] > wal[0][0]:
        stats["wal_sync_ms"] = round((wal[-1][1] - wal[0][1]) / (wal[-1][0] - wal[0][0]), 3)
    walw = [(float(r["wal_write"]), float(r["wal_write_time"])) for r in rows
            if r.get("wal_write") not in ("", None)]
    if len(walw) >= 2 and walw[-1][0] > walw[0][0]:
        stats["wal_write_ms"] = round((walw[-1][1] - walw[0][1]) / (walw[-1][0] - walw[0][0]), 3)
    disk = [(float(r["disk_writes"]), float(r["disk_write_ms"])) for r in rows
            if r.get("disk_writes") not in ("", None)]
    if len(disk) >= 2 and disk[-1][0] > disk[0][0]:
        stats["disk_write_ms"] = round((disk[-1][1] - disk[0][1]) / (disk[-1][0] - disk[0][0]), 3)

    def rate(column, scale=1.0):
        points = [(float(r["t_rel_ms"]), float(r[column])) for r in rows
                  if r.get(column) not in ("", None)]
        if len(points) >= 2 and points[-1][0] > points[0][0]:
            return round((points[-1][1] - points[0][1]) / ((points[-1][0] - points[0][0]) / 1000.0) / scale, 2)
        return "MISSING"

    stats["xact_per_sec"] = rate("xact_commit")
    stats["wal_sync_per_sec"] = rate("wal_sync")
    stats["wal_mb_per_sec"] = rate("wal_bytes", 1e6)
    stats["disk_writes_per_sec"] = rate("disk_writes")
    return stats


def recovery_stats(path):
    out = {"max_health_ms": "MISSING", "recovery_ms": "MISSING", "samples": 0}
    if not os.path.exists(path):
        return out
    rows = []
    with open(path) as fh:
        for line in fh:
            parts = line.strip().split(",")
            if len(parts) < 4:
                continue
            try:
                rows.append((int(parts[0]), int(parts[2]), float(parts[3]) * 1000))
            except ValueError:
                continue
    if not rows:
        return out
    out["samples"] = len(rows)
    out["max_health_ms"] = round(max(r[2] for r in rows), 2)
    baseline = sorted(r[2] for r in rows[-5:])[len(rows[-5:]) // 2]
    threshold = max(baseline * 3, 5.0)
    peak_idx = max(range(len(rows)), key=lambda i: rows[i][2])
    recovered_at = "never"
    for idx in range(peak_idx, len(rows)):
        if all(r[2] <= threshold for r in rows[idx:]):
            recovered_at = rows[idx][0]
            break
    out["recovery_ms"] = recovered_at
    out["baseline_ms"] = round(baseline, 2)
    out["peak_at_ms"] = rows[peak_idx][0]
    return out


def main():
    results_dir = sys.argv[1] if len(sys.argv) > 1 else "/tmp/diag/results"
    cases = []
    for name in sorted(os.listdir(results_dir)):
        case_dir = os.path.join(results_dir, name)
        summary_path = os.path.join(case_dir, "summary.json")
        if not os.path.exists(summary_path):
            continue
        with open(summary_path) as fh:
            data = json.load(fh)
        metrics = data.get("metrics", {})

        def count(metric):
            return int(metrics.get(metric, {}).get("values", {}).get("count") or 0)

        def value(metric, key, default=0):
            v = metrics.get(metric, {}).get("values", {}).get(key)
            return default if v is None else v

        meta = load_meta(os.path.join(case_dir, "meta.txt"))
        before = load_snapshot(os.path.join(case_dir, "before.txt"))
        after = load_snapshot(os.path.join(case_dir, "after.txt"))
        products = [int(p) for p in meta.get("products", "").split(",") if p]

        committed = sum(after.get("orders_%d" % p, 0) - before.get("orders_%d" % p, 0) for p in products)
        stock_delta = sum(before.get("stock_%d" % p, 0) - after.get("stock_%d" % p, 0) for p in products)
        duration_s = max(data.get("state", {}).get("testRunDurationMs", 0) / 1000.0, 0.001)

        row = {
            "label": name,
            "kind": "burst" if name.startswith("burst") else "rate",
            "target_rate": meta.get("target_rate", ""),
            "products": meta.get("products", ""),
            "started": count("iterations"),
            "scheduled": count("iterations") + count("dropped_iterations"),
            "completed": count("http_reqs"),
            "created_201": count("orders_created_201"),
            "achieved_rps": round(value("http_reqs", "rate"), 2),
            "duration_s": round(duration_s, 2),
            "committed_orders": committed,
            "committed_per_sec": round(committed / duration_s, 2),
            "oos_409": count("orders_out_of_stock_409"),
            "server_5xx": count("orders_server_error_5xx"),
            "other_status": count("orders_other_status"),
            "network_errors": count("orders_network_error"),
            "timeouts": count("orders_timeout"),
            "dropped": count("dropped_iterations"),
            "p50_ms": round(value("http_req_duration", "med", 0), 2),
            "p95_ms": round(value("http_req_duration", "p(95)", 0), 2),
            "p99_ms": round(value("http_req_duration", "p(99)", 0), 2),
            "under_100ms": count("latency_under_100ms"),
            "under_500ms": count("latency_under_500ms"),
            "under_1s": count("latency_under_1s"),
            "stock_delta": stock_delta,
            "stock_match": "yes" if committed == stock_delta else "NO",
            "final_stock": after.get("stock_%d" % products[0], 0) if len(products) == 1 else "",
            "k6_exit": meta.get("k6_exit", ""),
        }
        row.update(sampler_stats(os.path.join(case_dir, "samples.csv"), duration_s * 1000 + 500))
        row.update(recovery_stats(os.path.join(case_dir, "recovery.csv")))
        cases.append(row)

    if not cases:
        print("no cases found in %s" % results_dir)
        return

    fields = list(cases[0].keys())
    with open(os.path.join(results_dir, "results.csv"), "w", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=fields)
        writer.writeheader()
        writer.writerows(cases)

    print("## Traffic and correctness\n")
    print("| Case | Target | Scheduled | Started | Completed | 201 | Achieved/s | Committed | Committed/s | 409 | 5xx | Other | Timeout | Dropped | p50 ms | p95 ms | p99 ms | <100 ms | <500 ms | <1 s | Stock match |")
    print("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|")
    for r in cases:
        print("| {label} | {target_rate} | {scheduled} | {started} | {completed} | {created_201} | {achieved_rps} | {committed_orders} | {committed_per_sec} | {oos_409} | {server_5xx} | {other_status} | {timeouts} | {dropped} | {p50_ms} | {p95_ms} | {p99_ms} | {under_100ms} | {under_500ms} | {under_1s} | {stock_match} |".format(**r))

    print("\n## Resources (shared host: k6 runs on the same 2 vCPU box)\n")
    print("| Case | API CPU avg/max % | PG CPU avg/max % | k6 CPU avg % | API RSS MB | PG conns max | PG active max | Lock waits max | WAL fsync/s | WAL MB/s | WAL sync ms | Disk write ms | Disk writes/s | Load1 max | Recovery ms |")
    print("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|")
    for r in cases:
        print("| {label} | {api_cpu_avg}/{api_cpu_max} | {pg_cpu_avg}/{pg_cpu_max} | {k6_cpu_avg} | {api_rss_mb} | {pg_conns_max} | {pg_active_max} | {pg_lock_wait_max} | {wal_sync_per_sec} | {wal_mb_per_sec} | {wal_sync_ms} | {disk_write_ms} | {disk_writes_per_sec} | {load1_max} | {recovery_ms} |".format(**r))

    print("\nWrote %s" % os.path.join(results_dir, "results.csv"))


if __name__ == "__main__":
    main()
