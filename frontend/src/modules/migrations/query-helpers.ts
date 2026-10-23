import type { MigrationStatus } from '@/modules/migrations/types'

export const migrationQueryKeys = {
  all: (databaseId: string) => ['migrations', databaseId] as const,
  detail: (databaseId: string, migrationId: string) => ['migrations', databaseId, migrationId] as const,
}

const statusLabels: Record<MigrationStatus, string> = {
  draft: 'Draft',
  running: 'Berjalan',
  succeeded: 'Berhasil',
  failed: 'Gagal (rollback)',
  interrupted: 'Terputus — perlu rekonsiliasi',
}

export function migrationStatusLabel(status: MigrationStatus) {
  return statusLabels[status]
}
