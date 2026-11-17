// Pure /health capacity probe (no database involved).
import http from 'k6/http';

const BASE = __ENV.BASE_URL || 'http://127.0.0.1:8080';
const RATE = Number(__ENV.RATE || 10000);
const DUR = __ENV.DUR || '10s';

export const options = {
  scenarios: {
    health: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DUR,
      preAllocatedVUs: Math.max(100, Math.ceil(RATE / 100)),
      maxVUs: Math.max(500, Math.ceil(RATE / 10)),
    },
  },
  summaryTrendStats: ['avg', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  http.get(`${BASE}/health`);
}
