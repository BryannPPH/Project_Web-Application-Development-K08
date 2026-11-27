import { describeError } from '@/components/error-message'
import { Icon, type IconName } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { isTerminalJob, jobStatusLabel } from '@/modules/migrations/jobs'
import type { BatchProgress, ExecutionAction, JobStatus, MigrationJob } from '@/modules/migrations/types'

const actionLabel: Record<ExecutionAction, string> = {
  apply: 'Eksekusi',
  retry: 'Eksekusi ulang',
  rollback: 'Rollback',
  forward: 'Forward',
}

const statusIcon: Record<JobStatus, IconName> = {
  queued: 'history',
  running: 'loader',
  retrying: 'refresh',
  succeeded: 'check',
  failed: 'error',
  partial: 'alert',
  partially_reverted: 'alert',
  needs_reconciliation: 'alert',
}

function toneFor(status: JobStatus) {
  if (status === 'succeeded') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'queued' || status === 'running') return 'info'
  return 'warning'
}

// Pesan untuk kejadian yang bukan salah SQL: approval berubah saat job menunggu di queue.
function failureHint(job: MigrationJob) {
  if (job.errorCode === 'APPROVAL_REQUIRED') {
    return 'Worker menolak menjalankan job karena approval berubah atau sudah basi (SQL diedit, atau ada leader yang mengubah keputusan). Periksa panel approval, minta persetujuan ulang, lalu jalankan kembali.'
  }
  if (job.errorCode === 'RETRY_LIMIT_REACHED') {
    return 'Job sudah dicoba ulang sampai batas maksimum. Periksa pesan error di bawah sebelum mencoba lagi.'
  }
  return null
}

interface JobStatusPanelProps {
  job: MigrationJob | undefined
  loading: boolean
  error: unknown
}

export function JobStatusPanel({ job, loading, error }: JobStatusPanelProps) {
  if (error) return <StatusMessage tone="error">{describeError(error)}</StatusMessage>
  if (loading || !job) return <StatusMessage tone="loading">Menunggu job masuk antrean...</StatusMessage>

  const tone = toneFor(job.status)
  const done = isTerminalJob(job.status)
  const hint = failureHint(job)
  const meta = job.status === 'queued'
    ? 'Menunggu worker · halaman ini boleh ditutup.'
    : !done
      ? 'SQL dijalankan di worker, bukan di halaman ini.'
      : null
  return (
    <section aria-label="Status job" className={`job-status job-status--${tone}`} role="status">
      <Icon className={job.status === 'running' ? 'spinner' : undefined} name={statusIcon[job.status]} size={18} />
      <div className="job-status__body">
        <strong>
          {actionLabel[job.action]}: {jobStatusLabel(job)}
        </strong>
        {meta ? <span className="job-status__meta">{meta}</span> : null}
        {hint ? <p>{hint}</p> : null}
        {job.errorMessage && (job.status === 'failed' || job.status === 'retrying' || job.status === 'needs_reconciliation') ? (
          <pre>{job.errorMessage}</pre>
        ) : null}
      </div>
    </section>
  )
}

const batchStateText: Record<string, string> = {
  running: 'Sedang berjalan',
  queued: 'Menunggu worker',
  succeeded: 'Selesai',
  partial: 'Partial: berhenti di tengah, bisa dilanjutkan dari checkpoint',
  partially_reverted: 'Partially reverted: rollback berhenti di tengah, bisa dilanjutkan',
  failed: 'Gagal',
  needs_reconciliation: 'Perlu rekonsiliasi',
}

export function BatchProgressPanel({ progress, action }: { progress: BatchProgress; action?: ExecutionAction }) {
  const number = new Intl.NumberFormat('id-ID')
  const reverting = action === 'rollback' || progress.status === 'partially_reverted'
  const live = progress.status === 'running' || progress.status === 'queued'
  return (
    <section aria-label="Progres batch" className="batch-progress">
      <div className="batch-progress__head">
        <h3>Progres batch</h3>
        <span className="muted">{batchStateText[progress.status] ?? progress.status}</span>
      </div>
      <div
        aria-label={reverting ? 'Batch yang sudah dibatalkan' : 'Batch yang sudah committed'}
        aria-valuetext={`${number.format(progress.committedBatches)} batch`}
        className={`batch-progress__bar${live ? ' is-live' : ''}`}
        role="progressbar"
      >
        <span />
      </div>
      <dl className="batch-progress__stats">
        <div>
          <dt>{reverting ? 'Batch dibatalkan' : 'Batch committed'}</dt>
          <dd className="tabular">{number.format(progress.committedBatches)}</dd>
        </div>
        <div>
          <dt>{reverting ? 'Row dibatalkan' : 'Row committed'}</dt>
          <dd className="tabular">{number.format(progress.committedRows)}</dd>
        </div>
        <div>
          <dt>Cursor terakhir</dt>
          <dd className="tabular">{number.format(progress.lastCursor)}</dd>
        </div>
      </dl>
      <p className="muted batch-progress__note">
        Total row tidak diketahui sebelum batch terakhir selesai, jadi progres ditampilkan sebagai hitungan, bukan persen.
      </p>
    </section>
  )
}
