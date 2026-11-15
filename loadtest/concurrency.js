// README concurrency check: 500 POSTs for the last 100 units of stock,
// all fired inside roughly one second.
//
// Expected on a fresh DB: 100 x HTTP 201, 400 x HTTP 409, 100 orders, stock 0.
// Reset first: psql $env:DATABASE_URL -f k6/reset.sql
// Run: k6 run k6/concurrency.js
//
// Why 5 x 100 VUs instead of one 500-VU burst: on Windows the Go listener
// refuses connections when ~400+ new TCP connections arrive in the same
// instant, which would show up as fake "connection refused" failures.
// Staggering by 250ms keeps the race intact and removes that artifact.
import http from 'k6/http';
import { Counter } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';

export const created201 = new Counter('orders_created_201');
export const conflict409 = new Counter('orders_conflict_409');
export const other = new Counter('orders_other');

const wave = (startTime) => ({
  executor: 'per-vu-iterations',
  vus: 100,
  iterations: 1,
  maxDuration: '30s',
  startTime,
});

export const options = {
  scenarios: {
    wave1: wave('0s'),
    wave2: wave('250ms'),
    wave3: wave('500ms'),
    wave4: wave('750ms'),
    wave5: wave('1s'),
  },
};

export default function () {
  const res = http.post(
    `${BASE}/orders`,
    JSON.stringify({ product_id: 1, quantity: 1 }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (res.status === 201) created201.add(1);
  else if (res.status === 409) conflict409.add(1);
  else other.add(1);
}
