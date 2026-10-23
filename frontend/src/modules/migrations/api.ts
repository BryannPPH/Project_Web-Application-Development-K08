import { apiRequest } from '@/lib/api-client'
import type { DraftInput, MigrationEntry, MigrationPage } from '@/modules/migrations/types'

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
