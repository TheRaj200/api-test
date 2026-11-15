// Write-capacity stress test: POST /orders throughput (DB transaction path).
// IMPORTANT: run before this to avoid instant stock exhaustion (409s):
//   UPDATE products SET stock = 10000000 WHERE id = 1;
// Run: k6 run k6/stress-write.js
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';

export const options = {
  scenarios: {
    write_stress: {
      executor: 'ramping-arrival-rate',
      startRate: 5,
      timeUnit: '1s',
      preAllocatedVUs: 100,
      maxVUs: 2000,
      stages: [
        { target: 50,  duration: '30s' },
        { target: 100, duration: '30s' },
        { target: 200, duration: '30s' },
        { target: 400, duration: '30s' },
        { target: 600, duration: '30s' },
        { target: 0,   duration: '10s' },
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],
    http_req_duration: ['p(95)<1000'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const res = http.post(
    `${BASE}/orders`,
    JSON.stringify({ product_id: 1, quantity: 1 }),
    { headers: { 'Content-Type': 'application/json' }, tags: { name: 'POST /orders' } }
  );
  check(res, {
    'created 201': (r) => r.status === 201,
    'no 5xx': (r) => r.status < 500,
  });
}
