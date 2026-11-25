import type { ApprovalAction, ExecutionAction, MigrationEntry } from '@/modules/migrations/types'

export interface PendingAction {
  /** Aksi yang dikirim ke backend. */
  action: ExecutionAction
  /** Approval yang dibutuhkan; retry memakai approval apply. */
  approval: ApprovalAction
  label: (version: number) => string
  runningLabel: string
  confirmTitle: (version: number, databaseName: string) => string
  confirmNote: string
}

const apply: PendingAction = {
  action: 'apply',
  approval: 'apply',
  label: (version) => `Jalankan v${version}`,
  runningLabel: 'Mengantrekan...',
  confirmTitle: (version, db) => `Jalankan v${version} pada ${db}?`,
  confirmNote: 'Draft tidak dapat diedit lagi setelah dijalankan.',
}

const retry: PendingAction = {
  action: 'retry',
  approval: 'apply',
  label: (version) => `Coba ulang v${version}`,
  runningLabel: 'Mengantrekan...',
  confirmTitle: (version, db) => `Coba ulang v${version} pada ${db}?`,
  confirmNote: 'Execution dilanjutkan dari checkpoint terakhir jika migration ini batch.',
}

const rollback: PendingAction = {
  action: 'rollback',
  approval: 'rollback',
  label: (version) => `Rollback v${version}`,
  runningLabel: 'Mengantrekan...',
  confirmTitle: (version, db) => `Rollback v${version} pada ${db}?`,
  confirmNote: 'Down SQL dijalankan untuk membatalkan perubahan migration ini.',
}

const forward: PendingAction = {
  action: 'forward',
  approval: 'forward',
  label: (version) => `Jalankan ulang v${version}`,
  runningLabel: 'Mengantrekan...',
  confirmTitle: (version, db) => `Jalankan ulang (forward) v${version} pada ${db}?`,
  confirmNote: 'SQL migration dijalankan lagi setelah rollback. Forward adalah aksi baru dan butuh approval sendiri.',
}

/**
 * Aksi berikutnya yang boleh dikerjakan editor untuk migration ini. Mengikuti syarat
 * status di backend (migration.PlanExecution): backend tetap sumber kebenaran, ini hanya
 * menentukan tombol dan approval mana yang ditampilkan.
 */
export function nextAction(entry: Pick<MigrationEntry, 'status' | 'isActive' | 'downSql' | 'irreversibleReason'>): PendingAction | null {
  switch (entry.status) {
    case 'draft':
      return apply
    case 'failed':
    case 'partial':
      return retry
    case 'succeeded':
    case 'partially_reverted':
      // Tanpa down SQL (atau ditandai irreversible) rollback memang ditolak backend.
      return entry.isActive !== false && entry.downSql != null && entry.irreversibleReason == null ? rollback : null
    case 'rolled_back':
      return forward
    default:
      return null
  }
}
