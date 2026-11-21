import { apiRequest } from '@/lib/api-client'
import type {
  ApprovalAction,
  ApprovalInput,
  ApprovalReview,
  DraftInput,
  ExplainResult,
  MigrationEntry,
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

export function executeMigration(databaseId: string, migrationId: string) {
  return apiRequest<MigrationEntry>(migrationPath(databaseId, `/${encodeURIComponent(migrationId)}/execute`), {
    method: 'POST',
  })
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
