import { useQuery } from '@tanstack/react-query'

import { getApprovalReview } from '@/modules/migrations/api'
import { migrationQueryKeys } from '@/modules/migrations/query-helpers'
import type { ApprovalReview } from '@/modules/migrations/types'

export function useApprovalReview(databaseId: string, migrationId: string, enabled: boolean) {
  return useQuery({
    queryKey: migrationQueryKeys.approval(databaseId, migrationId, 'apply'),
    queryFn: ({ signal }) => getApprovalReview(databaseId, migrationId, 'apply', signal),
    enabled,
  })
}

/** Satu kalimat yang menjelaskan kenapa tombol Jalankan terkunci (dipakai di header). */
export function executionBlockedReason(review: ApprovalReview | undefined): string | null {
  if (!review) return 'Memuat status approval...'
  switch (review.status) {
    case 'approved':
      return null
    case 'pending':
      return `Menunggu persetujuan ${review.requiredCount - review.approvedCount} dari ${review.requiredCount} leader untuk revisi ${review.revision}.`
    case 'rejected':
      return 'Ada leader yang menolak revisi ini. Perbaiki SQL (revisi baru) lalu minta approval ulang.'
    case 'blocked':
      return 'Ada group ber-akses yang belum punya leader. Minta admin memilih leader.'
  }
}

