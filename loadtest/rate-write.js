// Fixed-rate write tier: POST /orders at exactly RATE req/s.
// Top up stock first: psql $env:DATABASE_URL -f k6/restock-high.sql
// Run: k6 run -e RATE=1500 -e BASE_URL=http://127.0.0.1:8090 k6/rate-write.js
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';
const RATE = Number(__ENV.RATE || 200);
const DUR = __ENV.DUR || '20s';

export const options = {
  scenarios: {
    write: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DUR,
      preAllocatedVUs: Math.max(50, Math.ceil(RATE / 20)),
      maxVUs: Math.max(200, Math.ceil(RATE / 2)),
    },
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const res = http.post(
    `${BASE}/orders`,
    JSON.stringify({ product_id: 1, quantity: 1 }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(res, {
    'created 201': (r) => r.status === 201,
    'no 5xx': (r) => r.status < 500,
  });
}
