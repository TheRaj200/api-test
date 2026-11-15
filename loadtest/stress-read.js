// Read-capacity stress test: how many requests/sec can the API sustain?
// Run: k6 run k6/stress-read.js
// Optional: k6 run -e BASE_URL=http://localhost:8080 k6/stress-read.js
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';

export const options = {
  scenarios: {
    read_stress: {
      executor: 'ramping-arrival-rate',
      startRate: 10,
      timeUnit: '1s',
      preAllocatedVUs: 200,
      maxVUs: 3000,
      stages: [
        { target: 200,  duration: '30s' }, // warm-up
        { target: 400,  duration: '30s' },
        { target: 800,  duration: '30s' },
        { target: 1600, duration: '30s' },
        { target: 2400, duration: '30s' },
        { target: 3000, duration: '30s' },
        { target: 0,    duration: '10s' }, // ramp-down
      ],
    },
  },
  thresholds: {
    // These will be crossed on purpose once the machine saturates.
    http_req_failed: ['rate<0.05'],
    http_req_duration: ['p(95)<1000'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
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
    // a missing order is a valid answer, not a failure
    responseCallback: http.expectedStatuses(200, 404),
  });
  check(res, { 'order 200/404': (r) => r.status === 200 || r.status === 404 });
}
