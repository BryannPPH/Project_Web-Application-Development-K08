import type { MigrationStatus } from '@/modules/migrations/types'

export const migrationQueryKeys = {
  all: (databaseId: string) => ['migrations', databaseId] as const,
  detail: (databaseId: string, migrationId: string) => ['migrations', databaseId, migrationId] as const,
}

const statusLabels: Record<MigrationStatus, string> = {
  draft: 'Draft',
  running: 'Berjalan…',
  succeeded: 'Berhasil',
  failed: 'Gagal · di-rollback',
  interrupted: 'Terputus',
  needs_reconciliation: 'Perlu rekonsiliasi',
}

export function migrationStatusLabel(status: MigrationStatus) {
  return statusLabels[status]
}

export function displayTime(value: string | null) {
  return value
    ? new Intl.DateTimeFormat('id-ID', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
    : '—'
}
