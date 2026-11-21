import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useId, useState } from 'react'

import { AccessBadge } from '@/components/Badges'
import { describeError } from '@/components/error-message'
import { Icon, type IconName } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { ApiError } from '@/lib/api-client'
import { decideApproval } from '@/modules/migrations/api'
import { displayTime, migrationQueryKeys } from '@/modules/migrations/query-helpers'
import { compatibilityHints } from '@/modules/migrations/review-hints'
import type { ApprovalDecision, ApprovalGroup, ApprovalReview, ApprovalStatus } from '@/modules/migrations/types'

const statusView: Record<ApprovalStatus, { className: string; icon: IconName; label: (r: ApprovalReview) => string }> = {
  approved: { className: 'badge badge--success', icon: 'check', label: () => 'Disetujui semua leader' },
  pending: {
    className: 'badge badge--warning',
    icon: 'history',
    label: (r) => `Menunggu ${r.requiredCount - r.approvedCount} dari ${r.requiredCount} leader`,
  },
  rejected: { className: 'badge badge--danger', icon: 'error', label: () => 'Ditolak' },
  blocked: { className: 'badge badge--danger', icon: 'lock', label: () => 'Terblokir: ada group tanpa leader' },
}

interface ApprovalPanelProps {
  databaseId: string
  migrationId: string
  databaseName: string
  sql: string
  review: ApprovalReview | undefined
  loading: boolean
  error: unknown
}

export function ApprovalPanel({ databaseId, migrationId, databaseName, sql, review, loading, error }: ApprovalPanelProps) {
  const titleId = useId()

  return (
    <section aria-labelledby={titleId} className="panel approval-panel">
      <div className="approval-panel__head">
        <h3 id={titleId}>Review &amp; approval</h3>
        {review ? <ApprovalStatusBadge review={review} /> : null}
      </div>

      {loading ? <StatusMessage tone="loading">Memuat status approval...</StatusMessage> : null}
      {error ? <StatusMessage tone="error">{describeError(error)}</StatusMessage> : null}

      {review ? (
        <>
          <p className="approval-panel__lead">
            Revisi <strong className="tabular">{review.revision}</strong>. Leader dari <strong>setiap</strong> group yang punya
            akses ke {databaseName} harus menyetujui revisi ini sebelum dijalankan. Mengedit SQL membuat revisi baru dan
            approval harus diulang.
          </p>

          <ul className="approval-groups">
            {review.groups.map((group) => (
              <GroupRow group={group} key={group.groupId} />
            ))}
          </ul>
          {review.groups.length === 0 ? (
            <StatusMessage tone="empty">Belum ada group yang punya akses ke database ini.</StatusMessage>
          ) : null}

          <CompatibilityHints sql={sql} />

          {review.canDecide ? (
            <DecisionForm databaseId={databaseId} migrationId={migrationId} review={review} />
          ) : (
            <p className="approval-panel__note muted">
              <Icon name="info" size={14} />
              Hanya leader group di atas yang dapat menyetujui atau menolak.
            </p>
          )}
        </>
      ) : null}
    </section>
  )
}

export function ApprovalStatusBadge({ review }: { review: ApprovalReview }) {
  const view = statusView[review.status]
  return (
    <span className={view.className}>
      <Icon name={view.icon} size={12} />
      {view.label(review)}
    </span>
  )
}

function GroupRow({ group }: { group: ApprovalGroup }) {
  const state = groupState(group)
  return (
    <li className={`approval-group approval-group--${state.tone}`}>
      <span aria-hidden="true" className="approval-group__icon">
        <Icon name={state.icon} size={14} />
      </span>
      <div className="approval-group__body">
        <div className="approval-group__title">
          <strong>{group.groupName}</strong>
          <AccessBadge level={group.accessLevel} />
        </div>
        <span className="approval-group__meta">
          {group.leader ? (
            <>
              Leader <strong>{group.leader.username}</strong> · {state.label}
              {group.decidedAt ? <> · {displayTime(group.decidedAt)}</> : null}
            </>
          ) : (
            'Belum ada leader. Admin perlu memilih leader di Admin → Group & akses.'
          )}
        </span>
        {group.note ? <q className="approval-group__note">{group.note}</q> : null}
      </div>
    </li>
  )
}

function groupState(group: ApprovalGroup): { tone: string; icon: IconName; label: string } {
  if (!group.leader) return { tone: 'blocked', icon: 'lock', label: 'tanpa leader' }
  if (group.decision === 'approved') return { tone: 'approved', icon: 'check', label: 'menyetujui' }
  if (group.decision === 'rejected') return { tone: 'rejected', icon: 'close', label: 'menolak' }
  return { tone: 'pending', icon: 'history', label: 'belum memutuskan' }
}

function CompatibilityHints({ sql }: { sql: string }) {
  const hints = compatibilityHints(sql)
  return (
    <div className="approval-hints">
      <h4>Periksa kompatibilitas</h4>
      {hints.length === 0 ? (
        <p className="muted">
          Tidak terdeteksi pola yang biasanya merusak query yang sudah ada. Tetap baca SQL di bawah sebelum memutuskan.
        </p>
      ) : (
        <ul>
          {hints.map((hint) => (
            <li className={`approval-hint approval-hint--${hint.level}`} key={hint.title}>
              <Icon name={hint.level === 'breaking' ? 'alert' : 'info'} size={14} />
              <div>
                <strong>{hint.title}</strong>
                <span>{hint.detail}</span>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function DecisionForm({ databaseId, migrationId, review }: { databaseId: string; migrationId: string; review: ApprovalReview }) {
  const queryClient = useQueryClient()
  const noteId = useId()
  const [note, setNote] = useState('')
  const [noteError, setNoteError] = useState<string | null>(null)
  const decide = useMutation({
    mutationFn: (decision: ApprovalDecision) =>
      decideApproval(databaseId, migrationId, {
        action: 'apply',
        revision: review.revision,
        decision,
        note: note.trim() || undefined,
      }),
    onSuccess: (updated) => {
      queryClient.setQueryData(migrationQueryKeys.approval(databaseId, migrationId, 'apply'), updated)
      setNote('')
    },
    onError: (error) => {
      // Revisi basi / status berubah: muat ulang migration dan review terbaru.
      if (error instanceof ApiError && (error.code === 'APPROVAL_STALE_REVISION' || error.code === 'APPROVAL_NOT_REVIEWABLE')) {
        void queryClient.invalidateQueries({ queryKey: migrationQueryKeys.detail(databaseId, migrationId) })
      }
    },
  })

  function submit(decision: ApprovalDecision) {
    if (decision === 'rejected' && note.trim() === '') {
      setNoteError('Tulis alasan penolakan supaya pembuat migration tahu apa yang harus diperbaiki.')
      return
    }
    setNoteError(null)
    decide.mutate(decision)
  }

  return (
    <div className="approval-decision">
      <h4>Keputusan Anda</h4>
      {review.myDecision ? (
        <p className="approval-decision__current">
          Anda sudah <strong>{review.myDecision === 'approved' ? 'menyetujui' : 'menolak'}</strong> revisi {review.revision}.
          Anda masih bisa mengubah keputusan selama migration belum dijalankan.
        </p>
      ) : null}
      <label className="approval-decision__label" htmlFor={noteId}>
        Catatan <span className="form-field__hint">(wajib jika menolak)</span>
      </label>
      <textarea
        aria-describedby={noteError ? `${noteId}-error` : undefined}
        aria-invalid={noteError ? true : undefined}
        className="approval-decision__note"
        id={noteId}
        maxLength={1000}
        onChange={(event) => setNote(event.target.value)}
        placeholder="Contoh: aman, kolom baru nullable dan tidak dipakai laporan kami."
        rows={3}
        value={note}
      />
      {noteError ? (
        <p className="form-error" id={`${noteId}-error`} role="alert">
          <Icon name="error" size={16} />
          {noteError}
        </p>
      ) : null}
      {decide.isError ? <StatusMessage tone="error">{describeError(decide.error)}</StatusMessage> : null}
      <div className="approval-decision__actions">
        {/* Keputusan yang sudah diambil jadi tombol "tenang" (nonaktif); aksi kebalikannya tetap tersedia. */}
        <button
          className={review.myDecision === 'approved' ? 'button button--secondary' : 'button button--primary'}
          disabled={decide.isPending || review.myDecision === 'approved'}
          onClick={() => submit('approved')}
          type="button"
        >
          <Icon className={decide.isPending && decide.variables === 'approved' ? 'spinner' : undefined}
            name={decide.isPending && decide.variables === 'approved' ? 'loader' : 'check'} size={16} />
          {review.myDecision === 'approved' ? 'Sudah disetujui' : `Setujui revisi ${review.revision}`}
        </button>
        <button
          className="button button--danger-outline"
          disabled={decide.isPending || review.myDecision === 'rejected'}
          onClick={() => submit('rejected')}
          type="button"
        >
          <Icon name="close" size={16} />
          {review.myDecision === 'rejected' ? 'Sudah ditolak' : review.myDecision === 'approved' ? 'Ubah jadi tolak' : 'Tolak'}
        </button>
      </div>
    </div>
  )
}
