import { apiRequest } from '@/lib/api-client'
import type {
  ApprovalAction,
  ApprovalInput,
  ApprovalReview,
  BatchProgress,
  DraftInput,
  ExecutionAction,
  ExecutionHistoryPage,
  ExplainResult,
  MigrationEntry,
  MigrationJob,
  MigrationPage,
  ReconciliationInput,
} from '@/modules/migrations/types'

function migrationPath(databaseId: string, suffix = '') {
  return `/databases/${encodeURIComponent(databaseId)}/migrations${suffix}`
}

export function listMigrations(databaseId: string, signal?: AbortSignal) {
  return apiRequest<MigrationPage>(migrationPath(databaseId), { signal })
}

export function getMigration(databaseId: string, migrationId: string, signal?: AbortSignal) {
  return apiRequest<MigrationEntry>(migrationPath(databaseId, `/${encodeURIComponent(migrationId)}`), { signal })
}

export function createMigration(databaseId: string, input: DraftInput) {
  return apiRequest<MigrationEntry>(migrationPath(databaseId), { method: 'POST', json: input })
}

export function updateMigration(databaseId: string, migrationId: string, input: DraftInput) {
  return apiRequest<MigrationEntry>(migrationPath(databaseId, `/${encodeURIComponent(migrationId)}`), {
    method: 'PATCH',
    json: input,
  })
}

const actionPaths: Record<ExecutionAction, string> = {
  apply: 'execute',
  retry: 'retry',
  rollback: 'rollback',
  forward: 'forward',
}

// Backend menjawab 202 + job: SQL berjalan di worker, bukan di request ini.
export function enqueueMigrationAction(databaseId: string, migrationId: string, action: ExecutionAction) {
  return apiRequest<MigrationJob>(
    migrationPath(databaseId, `/${encodeURIComponent(migrationId)}/${actionPaths[action]}`),
    { method: 'POST', headers: { 'Idempotency-Key': crypto.randomUUID() } },
  )
}

export function getMigrationJob(databaseId: string, jobId: string, signal?: AbortSignal) {
  return apiRequest<MigrationJob>(`/databases/${encodeURIComponent(databaseId)}/migration-jobs/${encodeURIComponent(jobId)}`, { signal })
}

export function getBatchProgress(databaseId: string, executionId: string, signal?: AbortSignal) {
  return apiRequest<BatchProgress>(
    `/databases/${encodeURIComponent(databaseId)}/migration-executions/${encodeURIComponent(executionId)}/progress`,
    { signal },
  )
}

export function listExecutionHistory(
  databaseId: string,
  options: { limit: number; offset: number; hideRollback: boolean },
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({
    limit: String(options.limit),
    offset: String(options.offset),
    hideRollback: String(options.hideRollback),
  })
  return apiRequest<ExecutionHistoryPage>(
    `/databases/${encodeURIComponent(databaseId)}/migration-history?${query.toString()}`,
    { signal },
  )
}

export function reconcileMigration(databaseId: string, migrationId: string, input: ReconciliationInput) {
  return apiRequest<MigrationEntry>(migrationPath(databaseId, `/${encodeURIComponent(migrationId)}/reconcile`), {
    method: 'POST',
    json: input,
  })
}

// Analisis plan berjalan di transaksi yang selalu di-rollback oleh backend.
export function explainMigrationSQL(databaseId: string, sql: string, analyze: boolean) {
  return apiRequest<ExplainResult>(migrationPath(databaseId, '/explain'), {
    method: 'POST',
    json: { sql, analyze },
  })
}

export async function getApprovalReview(databaseId: string, migrationId: string, action: ApprovalAction, signal?: AbortSignal) {
  const response = await apiRequest<{ review: ApprovalReview }>(
    migrationPath(databaseId, `/${encodeURIComponent(migrationId)}/approvals?action=${action}`),
    { signal },
  )
  return response.review
}

export async function decideApproval(databaseId: string, migrationId: string, input: ApprovalInput) {
  const response = await apiRequest<{ review: ApprovalReview }>(
    migrationPath(databaseId, `/${encodeURIComponent(migrationId)}/approvals`),
    { method: 'POST', json: input },
  )
  return response.review
}
