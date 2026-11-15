// Fixed-rate read tier: how does latency behave at exactly RATE req/s?
// Run: k6 run -e RATE=5000 -e BASE_URL=http://127.0.0.1:8090 k6/rate-read.js
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';
const RATE = Number(__ENV.RATE || 1000);
const DUR = __ENV.DUR || '20s';

export const options = {
  scenarios: {
    read: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DUR,
      preAllocatedVUs: Math.max(50, Math.ceil(RATE / 50)),
      maxVUs: Math.max(200, Math.ceil(RATE / 5)),
    },
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  if (Math.random() < 0.5) {
    const res = http.get(`${BASE}/health`, { tags: { name: 'GET /health' } });
    check(res, { 'health 200': (r) => r.status === 200 });
    return;
  }
  const id = Math.floor(Math.random() * 200) + 1;
  const res = http.get(`${BASE}/orders/${id}`, {
    tags: { name: 'GET /orders/{id}' },
    responseCallback: http.expectedStatuses(200, 404),
  });
  check(res, { 'order 200/404': (r) => r.status === 200 || r.status === 404 });
}
