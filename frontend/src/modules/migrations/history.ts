import type { ExecutionAttempt } from '@/modules/migrations/types'

/**
 * Filter tampilan "Hide rollback". Tidak menghapus data apa pun.
 *
 * Disembunyikan: execution yang sudah dibatalkan (ditunjuk `relatedExecutionId` oleh
 * rollback yang sukses) bersama rollback suksesnya. Selalu tampil: forward dan attempt
 * yang gagal/sebagian, juga rollback yang belum tuntas.
 */
export function hideRolledBack(items: ExecutionAttempt[]): ExecutionAttempt[] {
  const byId = new Map(items.map((item) => [item.executionId, item]))
  const hidden = new Set<string>()
  for (const item of items) {
    if (item.action !== 'rollback' || item.status !== 'succeeded' || !item.relatedExecutionId) continue
    const cancelled = byId.get(item.relatedExecutionId)
    // Target di luar halaman yang dimuat, atau forward: biarkan keduanya tampil supaya tidak yatim.
    if (!cancelled || cancelled.action === 'forward' || cancelled.status !== 'succeeded') continue
    hidden.add(cancelled.executionId)
    hidden.add(item.executionId)
  }
  return items.filter((item) => !hidden.has(item.executionId))
}
