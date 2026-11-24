import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate, SharedArray, Trend } from 'k6/metrics';

const cases = new SharedArray('approved migration jobs', () => {
  const path = __ENV.DBMP_CASES_FILE;
  return path ? JSON.parse(open(path)) : [];
});

const apiLatency = new Trend('api_latency_ms', true);
const apiErrorRate = new Rate('api_error_rate');
const jobExecutionErrorRate = new Rate('job_execution_error_rate');
const queueWait = new Trend('queue_wait_ms', true);
const jobDuration = new Trend('job_duration_ms', true);
const jobEndToEnd = new Trend('job_end_to_end_ms', true);
const jobsAccepted = new Counter('migration_jobs_accepted');
const jobsCompleted = new Counter('migration_jobs_completed');

const baseURL = (__ENV.DBMP_API_BASE_URL || 'http://localhost:8080').replace(/\/$/, '');
const cookieName = __ENV.DBMP_SESSION_COOKIE_NAME || 'dbmp_session';
const origin = __ENV.DBMP_ORIGIN || 'http://localhost:5173';
const pollIntervalSeconds = Number(__ENV.DBMP_POLL_INTERVAL_SECONDS || 2);
const maxPolls = Number(__ENV.DBMP_MAX_POLLS || 180);

export const options = {
  scenarios: {
    queue_ramp: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: [
        { duration: '1m', target: 1 },  // baseline
        { duration: '2m', target: 10 }, // above normal team load
        { duration: '3m', target: 30 }, // peak load
        { duration: '2m', target: 10 }, // ramp down and observe recovery
        { duration: '1m', target: 0 },
      ],
      gracefulRampDown: '2m',
    },
  },
  thresholds: {
    api_latency_ms: [`p(95)<${__ENV.K6_P95_API_MS || 1000}`],
    'api_latency_ms{kind:enqueue}': [`p(95)<${__ENV.K6_P95_API_MS || 1000}`],
    api_error_rate: [`rate<${__ENV.K6_API_ERROR_RATE || 0.02}`],
    http_req_failed: [`rate<${__ENV.K6_API_ERROR_RATE || 0.02}`],
    job_execution_error_rate: ['rate<0.05'],
    queue_wait_ms: ['p(95)<5000'],
    job_duration_ms: ['p(95)<60000'],
  },
};

const headers = {
  Cookie: `${cookieName}=${__ENV.DBMP_SESSION_COOKIE || ''}`,
  'X-CSRF-Token': __ENV.DBMP_CSRF_TOKEN || '',
  Origin: origin,
  'Content-Type': 'application/json',
};

const terminalStatuses = new Set([
  'succeeded',
  'failed',
  'partial',
  'partially_reverted',
  'needs_reconciliation',
]);

function recordResponse(kind, response, accepted) {
  apiLatency.add(response.timings.duration, { kind });
  apiErrorRate.add(!accepted);
}

export default function () {
  if (!__ENV.DBMP_SESSION_COOKIE || !__ENV.DBMP_CSRF_TOKEN || !__ENV.DBMP_CASES_FILE) {
    exec.test.abort('Set DBMP_SESSION_COOKIE, DBMP_CSRF_TOKEN, and DBMP_CASES_FILE.');
  }

  const caseIndex = exec.scenario.iterationInTest;
  if (caseIndex >= cases.length) {
    exec.test.abort('The approved migration manifest has fewer cases than the load test iterations.');
    return;
  }

  const testCase = cases[caseIndex];
  const databasePath = `/api/v1/databases/${testCase.databaseId}`;

  // Generate concurrent read traffic alongside queue submissions.
  const readResponse = http.get(`${baseURL}${databasePath}/migrations?limit=1`, { headers });
  recordResponse('read', readResponse, readResponse.status === 200);
  check(readResponse, { 'migration list is readable': (response) => response.status === 200 });
  if (readResponse.status !== 200) {
    sleep(0.2);
    return;
  }

  const actionPath = {
    apply: 'execute',
    retry: 'retry',
    rollback: 'rollback',
    forward: 'forward',
  }[testCase.action];
  if (!actionPath) {
    apiErrorRate.add(true);
    return;
  }

  const enqueueResponse = http.post(
    `${baseURL}${databasePath}/migrations/${testCase.migrationId}/${actionPath}`,
    '{}',
    { headers, tags: { name: 'migration_enqueue' } },
  );
  let enqueueJob;
  try {
    enqueueJob = enqueueResponse.json();
  } catch (_) {
    enqueueJob = null;
  }
  const accepted = enqueueResponse.status === 202 && enqueueJob && enqueueJob.jobId;
  recordResponse('enqueue', enqueueResponse, Boolean(accepted));
  check(enqueueResponse, {
    'migration job accepted (202)': (response) => response.status === 202 && Boolean(enqueueJob?.jobId),
  });
  if (!accepted) {
    return;
  }
  jobsAccepted.add(1);

  const jobURL = `${baseURL}${databasePath}/migration-jobs/${enqueueJob.jobId}`;
  for (let poll = 0; poll < maxPolls; poll += 1) {
    sleep(pollIntervalSeconds);
    const statusResponse = http.get(jobURL, { headers, tags: { name: 'migration_job_status' } });
    let job;
    try {
      job = statusResponse.json();
    } catch (_) {
      job = null;
    }
    const readable = statusResponse.status === 200 && job;
    recordResponse('poll', statusResponse, Boolean(readable));
    if (!readable) {
      continue;
    }
    if (!terminalStatuses.has(job.status)) {
      continue;
    }

    jobsCompleted.add(1, { status: job.status });
    const succeeded = job.status === 'succeeded';
    jobExecutionErrorRate.add(!succeeded);
    if (job.createdAt && job.startedAt) {
      queueWait.add(Date.parse(job.startedAt) - Date.parse(job.createdAt));
    }
    if (job.startedAt && job.finishedAt) {
      jobDuration.add(Date.parse(job.finishedAt) - Date.parse(job.startedAt));
    }
    if (job.createdAt && job.finishedAt) {
      jobEndToEnd.add(Date.parse(job.finishedAt) - Date.parse(job.createdAt));
    }
    check(job, { 'migration execution succeeded': (value) => value.status === 'succeeded' });
    return;
  }

  jobExecutionErrorRate.add(true);
  apiErrorRate.add(true);
}
