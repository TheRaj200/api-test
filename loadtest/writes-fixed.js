// Fixed-rate write test that can spread load across one or more products.
//
//   k6 run -e RATE=800 -e DUR=60s -e PRODUCT_IDS=9001 loadtest/writes-fixed.js
//   k6 run -e RATE=800 -e DUR=60s -e PRODUCT_IDS=9001,9002,9003,9004 loadtest/writes-fixed.js
//
// Every request is one order of quantity 1. 201 = committed order,
// 409 = valid out-of-stock, 503 = server-side database timeout.
//
// SUMMARY_PATH writes the full k6 summary JSON (used by diagnostics/analyze.py).
import http from 'k6/http';
import { Counter } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://127.0.0.1:8080';
const RATE = Number(__ENV.RATE || 500);
const DUR = __ENV.DUR || '60s';
const PRODUCT_IDS = (__ENV.PRODUCT_IDS || '9001').split(',').map(Number);
const SUMMARY_PATH = __ENV.SUMMARY_PATH || '';

export const created201 = new Counter('orders_created_201');
export const outOfStock409 = new Counter('orders_out_of_stock_409');
export const serverErrors5xx = new Counter('orders_server_error_5xx');
export const otherStatus = new Counter('orders_other_status');
export const networkErrors = new Counter('orders_network_error');
export const timeoutErrors = new Counter('orders_timeout');
export const under100ms = new Counter('latency_under_100ms');
export const under500ms = new Counter('latency_under_500ms');
export const under1s = new Counter('latency_under_1s');

export const options = {
  scenarios: {
    write: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DUR,
      preAllocatedVUs: Math.max(200, Math.ceil(RATE / 2)),
      // Generous on purpose: we want server saturation, not a k6 VU cap.
      maxVUs: Math.max(1000, RATE * 2),
    },
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const productID = PRODUCT_IDS[Math.floor(Math.random() * PRODUCT_IDS.length)];
  const res = http.post(
    `${BASE}/orders`,
    JSON.stringify({ product_id: productID, quantity: 1 }),
    { headers: { 'Content-Type': 'application/json' }, timeout: '10s' }
  );

  const duration = res.timings.duration;
  if (duration < 100) under100ms.add(1);
  if (duration < 500) under500ms.add(1);
  if (duration < 1000) under1s.add(1);

  if (res.status === 201) created201.add(1);
  else if (res.status === 409) outOfStock409.add(1);
  else if (res.status === 0) {
    networkErrors.add(1);
    if (res.error_code === 1050 || String(res.error || '').includes('timeout')) {
      timeoutErrors.add(1);
    }
  } else if (res.status >= 500) serverErrors5xx.add(1);
  else otherStatus.add(1);
}

export function handleSummary(data) {
  const c = (name) => (data.metrics[name] && data.metrics[name].values.count) || 0;
  console.log(
    `case_summary target_rate=${RATE} achieved_rps=${(data.metrics.http_reqs.values.rate || 0).toFixed(2)} ` +
      `created201=${c('orders_created_201')} oos409=${c('orders_out_of_stock_409')} ` +
      `server5xx=${c('orders_server_error_5xx')} other=${c('orders_other_status')} ` +
      `network=${c('orders_network_error')} timeouts=${c('orders_timeout')} ` +
      `dropped=${c('dropped_iterations')}`
  );
  if (!SUMMARY_PATH) return {};
  return { [SUMMARY_PATH]: JSON.stringify(data, null, 2) };
}
