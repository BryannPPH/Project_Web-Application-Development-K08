import type { ExecutionAction, MigrationStatus } from '@/modules/migrations/types'

export const migrationQueryKeys = {
  all: (databaseId: string) => ['migrations', databaseId] as const,
  list: (databaseId: string, offset: number) => ['migrations', databaseId, { offset }] as const,
  detail: (databaseId: string, migrationId: string) => ['migrations', databaseId, migrationId] as const,
  approval: (databaseId: string, migrationId: string, action: string) =>
    ['migrations', databaseId, migrationId, 'approval', action] as const,
}

const statusLabels: Record<MigrationStatus, string> = {
  draft: 'Draft',
  running: 'Berjalan...',
  succeeded: 'Berhasil',
  failed: 'Gagal · di-rollback',
  interrupted: 'Terputus',
  needs_reconciliation: 'Perlu rekonsiliasi',
  reconciled: 'Direkonsiliasi',
  rolled_back: 'Di-rollback',
  partial: 'Sebagian selesai',
  partially_reverted: 'Sebagian dibatalkan',
}

export function migrationStatusLabel(status: MigrationStatus) {
  return statusLabels[status]
}

export const executionActionText: Record<ExecutionAction, string> = {
  apply: 'Eksekusi',
  retry: 'Eksekusi ulang',
  rollback: 'Rollback',
  forward: 'Forward',
}

export const executionStatusText: Record<string, string> = {
  queued: 'Mengantre',
  running: 'Berjalan',
  succeeded: 'Berhasil',
  failed: 'Gagal',
  partial: 'Sebagian',
  partially_reverted: 'Sebagian dibatalkan',
  interrupted: 'Terputus',
  needs_reconciliation: 'Perlu rekonsiliasi',
}

export function displayTime(value: string | null) {
  return value
    ? new Intl.DateTimeFormat('id-ID', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
    : '-'
}
