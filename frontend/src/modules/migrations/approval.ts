import { useQuery } from '@tanstack/react-query'

import { getApprovalReview } from '@/modules/migrations/api'
import { migrationQueryKeys } from '@/modules/migrations/query-helpers'
import type { ApprovalAction, ApprovalReview } from '@/modules/migrations/types'

export function useApprovalReview(databaseId: string, migrationId: string, action: ApprovalAction | null) {
  return useQuery({
    queryKey: migrationQueryKeys.approval(databaseId, migrationId, action ?? 'none'),
    queryFn: ({ signal }) => getApprovalReview(databaseId, migrationId, action ?? 'apply', signal),
    enabled: action !== null && databaseId !== '' && migrationId !== '',
  })
}

const verb: Record<ApprovalAction, string> = { apply: 'dijalankan', rollback: 'di-rollback', forward: 'dijalankan ulang' }

/** Satu kalimat yang menjelaskan kenapa tombol aksi terkunci (dipakai di header). */
export function executionBlockedReason(review: ApprovalReview | undefined): string | null {
  if (!review) return 'Memuat status approval...'
  switch (review.status) {
    case 'approved':
      return null
    case 'pending':
      return `Menunggu persetujuan ${review.requiredCount - review.approvedCount} dari ${review.requiredCount} leader sebelum ${verb[review.action]} (revisi ${review.revision}).`
    case 'rejected':
      return 'Ada leader yang menolak. Perbaiki SQL (revisi baru) lalu minta approval ulang, atau ubah keputusan lewat panel review.'
    case 'blocked':
      return 'Ada group ber-akses yang belum punya leader. Minta admin memilih leader.'
  }
}
