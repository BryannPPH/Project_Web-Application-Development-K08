import { useQuery } from '@tanstack/react-query'

import { getBatchProgress, getMigrationJob } from '@/modules/migrations/api'
import type { JobStatus, MigrationJob } from '@/modules/migrations/types'

const terminal: JobStatus[] = ['succeeded', 'failed', 'partial', 'partially_reverted', 'needs_reconciliation']

export function isTerminalJob(status: JobStatus) {
  return terminal.includes(status)
}

export const jobQueryKeys = {
  job: (databaseId: string, jobId: string) => ['migration-job', databaseId, jobId] as const,
  progress: (databaseId: string, executionId: string) => ['migration-progress', databaseId, executionId] as const,
  history: (databaseId: string) => ['migration-history', databaseId] as const,
  historyPage: (databaseId: string, offset: number, hideRollback: boolean) =>
    ['migration-history', databaseId, { offset, hideRollback }] as const,
}

/** Memantau job sampai status akhir tanpa menahan request execute. */
export function useMigrationJob(databaseId: string, jobId: string | null) {
  return useQuery({
    queryKey: jobQueryKeys.job(databaseId, jobId ?? ''),
    queryFn: ({ signal }) => getMigrationJob(databaseId, jobId ?? '', signal),
    enabled: jobId !== null,
    refetchInterval: (query) => {
      const job = query.state.data as MigrationJob | undefined
      return job && isTerminalJob(job.status) ? false : 1500
    },
  })
}

export function useBatchProgress(databaseId: string, executionId: string | null | undefined, live: boolean) {
  return useQuery({
    queryKey: jobQueryKeys.progress(databaseId, executionId ?? ''),
    queryFn: ({ signal }) => getBatchProgress(databaseId, executionId ?? '', signal),
    enabled: Boolean(executionId),
    refetchInterval: live ? 2000 : false,
  })
}

const jobLabels: Record<JobStatus, string> = {
  queued: 'Mengantre',
  running: 'Berjalan',
  retrying: 'Mencoba ulang',
  succeeded: 'Selesai',
  failed: 'Gagal',
  partial: 'Sebagian selesai',
  partially_reverted: 'Sebagian dibatalkan',
  needs_reconciliation: 'Perlu rekonsiliasi',
}

export function jobStatusLabel(job: Pick<MigrationJob, 'status' | 'retryCount'>) {
  return job.status === 'retrying' ? `Mencoba ulang (ke-${job.retryCount})` : jobLabels[job.status]
}
