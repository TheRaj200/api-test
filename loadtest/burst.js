// Burst test: fire a fixed number of POST /orders requests as fast as possible.
//
//   k6 run -e REQUESTS=500 -e VUS=100 -e PRODUCT_ID=9201 loadtest/burst.js
//
// The product must be pre-loaded with exactly 100 units by
// diagnostics/setup-fixtures.sql. Each request orders quantity 1, so a healthy
// run commits exactly 100 orders and returns 409 for every other request that
// reaches the API. 201 = committed, 409 = valid out-of-stock, 503 = DB timeout,
// status 0 = network error / timeout.
import http from 'k6/http';
import { Counter } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://127.0.0.1:8080';
const REQUESTS = Number(__ENV.REQUESTS || 500);
const VUS = Number(__ENV.VUS || 100);
const PRODUCT_ID = Number(__ENV.PRODUCT_ID || 9201);
const MAX_DURATION = __ENV.MAX_DURATION || '120s';
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
    burst: {
      executor: 'shared-iterations',
      vus: VUS,
      iterations: REQUESTS,
      maxDuration: MAX_DURATION,
    },
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const res = http.post(
    `${BASE}/orders`,
    JSON.stringify({ product_id: PRODUCT_ID, quantity: 1 }),
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
    `burst_summary requests=${REQUESTS} vus=${VUS} started=${data.metrics.iterations.values.count} ` +
      `dropped=${c('dropped_iterations')} duration_ms=${data.state.testRunDurationMs} ` +
      `created201=${c('orders_created_201')} oos409=${c('orders_out_of_stock_409')} ` +
      `server5xx=${c('orders_server_error_5xx')} other=${c('orders_other_status')} ` +
      `network=${c('orders_network_error')} timeouts=${c('orders_timeout')}`
  );
  if (!SUMMARY_PATH) return {};
  return { [SUMMARY_PATH]: JSON.stringify(data, null, 2) };
}
