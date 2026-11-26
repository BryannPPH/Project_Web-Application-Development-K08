import http from 'k6/http';
import { check, sleep } from 'k6';
import { SharedArray } from 'k6/data';
import exec from 'k6/execution';
import { Counter, Rate, Trend } from 'k6/metrics';

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
const apiRequests = new Counter('api_requests');
const jobsAccepted = new Counter('migration_jobs_accepted');
const jobsCompleted = new Counter('migration_jobs_completed');
const caseManifestExhausted = new Counter('migration_case_manifest_exhausted');

const baseURL = (__ENV.DBMP_API_BASE_URL || 'http://localhost:8080').replace(/\/$/, '');
const cookieName = __ENV.DBMP_SESSION_COOKIE_NAME || 'dbmp_session';
const origin = __ENV.DBMP_ORIGIN || 'http://localhost:5173';
const pollIntervalSeconds = Number(__ENV.DBMP_POLL_INTERVAL_SECONDS || 2);
const maxPolls = Number(__ENV.DBMP_MAX_POLLS || 180);
const gracefulRampDownSeconds = maxPolls * pollIntervalSeconds + 10;

function loadStage() {
  const elapsed = exec.instance.currentTestRunDuration;
  if (elapsed < 60000) return 'baseline_1_vu';
  if (elapsed < 180000) return 'ramp_to_10_vu';
  if (elapsed < 360000) return 'peak_30_vu';
  if (elapsed < 480000) return 'ramp_down_to_10_vu';
  if (elapsed < 540000) return 'ramp_to_0_vu';
  return 'drain';
}

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
      gracefulRampDown: `${gracefulRampDownSeconds}s`,
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
    migration_case_manifest_exhausted: ['count<1'],
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
  const stage = loadStage();
  apiLatency.add(response.timings.duration, { kind, stage });
  apiErrorRate.add(!accepted, { stage });
  apiRequests.add(1, { kind, stage });
}

function waitForPredecessor(testCase, databasePath) {
  if (!testCase.predecessorMigrationId) return true;

  const predecessorURL = `${baseURL}${databasePath}/migrations/${encodeURIComponent(testCase.predecessorMigrationId)}`;
  for (let poll = 0; poll < maxPolls; poll += 1) {
    const response = http.get(predecessorURL, { headers, tags: { name: 'migration_predecessor_status' } });
    let predecessor;
    try {
      predecessor = response.json();
    } catch (_) {
      predecessor = null;
    }
    const readable = response.status === 200 && predecessor;
    recordResponse('dependency', response, Boolean(readable));
    if (!readable) {
      sleep(pollIntervalSeconds);
      continue;
    }
    if (predecessor.status === 'succeeded') return true;
    if (terminalStatuses.has(predecessor.status)) {
      jobExecutionErrorRate.add(true, { stage: loadStage() });
      check(predecessor, { 'predecessor migration succeeded': (value) => value.status === 'succeeded' });
      return false;
    }
    sleep(pollIntervalSeconds);
  }

  jobExecutionErrorRate.add(true, { stage: loadStage() });
  return false;
}

export default function () {
  if (!__ENV.DBMP_SESSION_COOKIE || !__ENV.DBMP_CSRF_TOKEN || !__ENV.DBMP_CASES_FILE) {
    exec.test.abort('Set DBMP_SESSION_COOKIE, DBMP_CSRF_TOKEN, and DBMP_CASES_FILE.');
  }

  if (cases.length === 0) {
    exec.test.abort('DBMP_CASES_FILE must contain at least one approved migration case.');
    return;
  }

  const caseIndex = exec.scenario.iterationInTest;
  const hasMigrationCase = caseIndex < cases.length;
  // Keep read traffic flowing if the unique migration manifest runs out, so the
  // full ramp still produces API measurements. A threshold marks that run invalid
  // as a queue stress result instead of silently treating reads as enqueue load.
  const testCase = hasMigrationCase ? cases[caseIndex] : cases[0];
  const databasePath = `/api/v1/databases/${testCase.databaseId}`;

  // Generate concurrent read traffic alongside queue submissions.
  const readResponse = http.get(`${baseURL}${databasePath}/migrations?limit=1`, { headers });
  recordResponse('read', readResponse, readResponse.status === 200);
  check(readResponse, { 'migration list is readable': (response) => response.status === 200 });
  if (readResponse.status !== 200) {
    sleep(0.2);
    return;
  }

  if (!hasMigrationCase) {
    caseManifestExhausted.add(1);
    sleep(pollIntervalSeconds);
    return;
  }

  if (!waitForPredecessor(testCase, databasePath)) return;
  const jobStage = loadStage();

  const actionPath = {
    apply: 'execute',
    retry: 'retry',
    rollback: 'rollback',
    forward: 'forward',
  }[testCase.action];
  if (!actionPath) {
    apiErrorRate.add(true, { stage: jobStage });
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
  jobsAccepted.add(1, { stage: jobStage });

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

    jobsCompleted.add(1, { status: job.status, stage: jobStage });
    const succeeded = job.status === 'succeeded';
    jobExecutionErrorRate.add(!succeeded, { stage: jobStage });
    if (job.createdAt && job.startedAt) {
      queueWait.add(Date.parse(job.startedAt) - Date.parse(job.createdAt), { stage: jobStage });
    }
    if (job.startedAt && job.finishedAt) {
      jobDuration.add(Date.parse(job.finishedAt) - Date.parse(job.startedAt), { stage: jobStage });
    }
    if (job.createdAt && job.finishedAt) {
      jobEndToEnd.add(Date.parse(job.finishedAt) - Date.parse(job.createdAt), { stage: jobStage });
    }
    check(job, { 'migration execution succeeded': (value) => value.status === 'succeeded' });
    return;
  }

  jobExecutionErrorRate.add(true, { stage: jobStage });
  apiErrorRate.add(true, { stage: jobStage });
}
