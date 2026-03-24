/**
 * k6 load test — nexus API gateway
 *
 * Tests:
 *   1. REST GET /api/v1/github/users/:login  (cached after first hit)
 *   2. GraphQL query { githubUser }           (same cache, different surface)
 *   3. GraphQL query { me }                   (hits Postgres, no cache)
 *
 * Run:
 *   k6 run tests/load/github_user.js \
 *     -e BASE_URL=http://localhost:8080 \
 *     -e JWT=<token>
 *
 * Stages: ramp 0→50 VUs over 30s, hold 50 VUs for 1m, ramp down 30s.
 *
 * Thresholds (fail the test if exceeded):
 *   - 95th percentile latency < 500ms
 *   - error rate < 1%
 */

import http from "k6/http";
import { check, sleep } from "k6";
import { Trend, Rate, Counter } from "k6/metrics";

// ── Custom metrics ────────────────────────────────────────────────────────────
const cacheHits = new Counter("cache_hits");
const cacheMisses = new Counter("cache_misses");
const graphqlErrors = new Counter("graphql_errors");
const restLatency = new Trend("rest_latency", true);
const graphqlLatency = new Trend("graphql_latency", true);
const errorRate = new Rate("error_rate");

// ── Config ────────────────────────────────────────────────────────────────────
const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";
const JWT = __ENV.JWT || "";
const LOGINS = ["torvalds", "gopher", "bradfitz", "rsc", "davecheney"];

export const options = {
  stages: [
    { duration: "30s", target: 50 },
    { duration: "1m", target: 50 },
    { duration: "30s", target: 0 },
  ],
  thresholds: {
    http_req_duration: ["p(95)<500"],
    error_rate: ["rate<0.01"],
    graphql_errors: ["count<5"],
  },
};

const headers = {
  Authorization: `Bearer ${JWT}`,
  "Content-Type": "application/json",
};

// ── VU logic ──────────────────────────────────────────────────────────────────
export default function () {
  const login = LOGINS[Math.floor(Math.random() * LOGINS.length)];

  // 1. REST endpoint
  restScenario(login);

  // 2. GraphQL githubUser
  graphqlGithubUser(login);

  // 3. GraphQL me (no cache — exercises DB path)
  graphqlMe();

  sleep(0.5);
}

function restScenario(login) {
  const start = Date.now();
  const res = http.get(`${BASE_URL}/api/v1/github/users/${login}`, { headers });
  restLatency.add(Date.now() - start);

  const ok = check(res, {
    "REST status 200": (r) => r.status === 200,
    "REST has body": (r) => r.body && r.body.length > 0,
  });

  errorRate.add(!ok);

  if (res.headers["X-Cache"] === "HIT") {
    cacheHits.add(1);
  } else {
    cacheMisses.add(1);
  }
}

function graphqlGithubUser(login) {
  const payload = JSON.stringify({
    query: `{ githubUser(login: "${login}") { login name publicRepos followers } }`,
  });

  const start = Date.now();
  const res = http.post(`${BASE_URL}/graphql`, payload, { headers });
  graphqlLatency.add(Date.now() - start);

  const body = res.json();
  const ok = check(res, {
    "GraphQL githubUser status 200": (r) => r.status === 200,
    "GraphQL githubUser no errors": () =>
      !body.errors || body.errors.length === 0,
  });

  errorRate.add(!ok);
  if (body.errors && body.errors.length > 0) {
    graphqlErrors.add(body.errors.length);
  }
}

function graphqlMe() {
  const payload = JSON.stringify({
    query: `{ me { id email savedSearches { query } } }`,
  });

  const start = Date.now();
  const res = http.post(`${BASE_URL}/graphql`, payload, { headers });
  graphqlLatency.add(Date.now() - start);

  const body = res.json();
  const ok = check(res, {
    "GraphQL me status 200": (r) => r.status === 200,
    "GraphQL me no errors": () => !body.errors || body.errors.length === 0,
  });

  errorRate.add(!ok);
}

// ── Summary ───────────────────────────────────────────────────────────────────
export function handleSummary(data) {
  const hits = data.metrics.cache_hits ? data.metrics.cache_hits.values.count : 0;
  const misses = data.metrics.cache_misses ? data.metrics.cache_misses.values.count : 0;
  const total = hits + misses;
  const hitRate = total > 0 ? ((hits / total) * 100).toFixed(1) : "n/a";

  console.log(`\n── Cache summary ────────────────`);
  console.log(`  HIT:  ${hits} (${hitRate}%)`);
  console.log(`  MISS: ${misses}`);
  console.log(`  Total REST requests: ${total}`);

  return {
    stdout: JSON.stringify(data, null, 2),
  };
}
