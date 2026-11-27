import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import { MigrationStatusBadge } from '@/components/Badges'
import { senja } from '@/components/brand'
import { Icon } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { ApiError } from '@/lib/api-client'
import { useAccessibleDatabase } from '@/modules/databases/api'
import { createMigration, enqueueMigrationAction, getMigration, reconcileMigration, updateMigration } from '@/modules/migrations/api'
import { availableActions } from '@/modules/migrations/actions'
import { executionBlockedReason, useApprovalReview } from '@/modules/migrations/approval'
import { ApprovalPanel } from '@/modules/migrations/ApprovalPanel'
import { ExplainPanel } from '@/modules/migrations/ExplainPanel'
import { isTerminalJob, jobQueryKeys, useBatchProgress, useMigrationJob } from '@/modules/migrations/jobs'
import { BatchProgressPanel, JobStatusPanel } from '@/modules/migrations/JobStatusPanel'
import { MigrationEditor } from '@/modules/migrations/MigrationEditor'
import { displayTime, migrationQueryKeys } from '@/modules/migrations/query-helpers'
import type { DraftInput, ExecutionAction, MigrationEntry } from '@/modules/migrations/types'
import '@/modules/migrations/migrations.css'

function ErrorMessage({ error }: { error: unknown }) {
  return (
    <StatusMessage tone="error">
      {error instanceof ApiError ? error.message : 'Permintaan migration gagal diproses.'}
    </StatusMessage>
  )
}

export function MigrationDetailPage({ create = false }: { create?: boolean }) {
  const { databaseId, migrationId } = useParams()
  return <MigrationDetail key={`${databaseId}/${migrationId ?? 'new'}`} create={create} />
}

function MigrationDetail({ create }: { create: boolean }) {
  const { databaseId = '', migrationId = '' } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  // Draft baru dapat diisi otomatis dari halaman schema diff.
  const prefill = create ? readPrefill(location.state) : undefined
  const [liveSql, setLiveSql] = useState<string | null>(null)
  const [draftDirty, setDraftDirty] = useState(false)
  const [reconciliationNote, setReconciliationNote] = useState('')
  const [targetInspected, setTargetInspected] = useState(false)
  const database = useAccessibleDatabase(databaseId)
  const detail = useQuery({
    queryKey: migrationQueryKeys.detail(databaseId, migrationId),
    queryFn: ({ signal }) => getMigration(databaseId, migrationId, signal),
    enabled: !create && databaseId !== '' && migrationId !== '',
    refetchOnMount: 'always',
  })
  // Status partial menawarkan resume dan rollback; approval mengikuti aksi yang dipilih.
  const pendingOptions = !create && detail.data ? availableActions(detail.data) : []
  const [selectedAction, setSelectedAction] = useState<ExecutionAction | null>(null)
  const pending = pendingOptions.find((option) => option.action === selectedAction) ?? pendingOptions[0] ?? null
  const approval = useApprovalReview(databaseId, migrationId, pending ? pending.approval : null)
  const jobId = detail.data?.latestJobId ?? null
  const job = useMigrationJob(databaseId, jobId)
  const jobDone = job.data ? isTerminalJob(job.data.status) : false
  const currentExecutionId = job.data?.executionId
  const batchExecutionId = detail.data?.executionMode === 'batch'
    ? currentExecutionId ?? job.data?.previousExecutionId ?? detail.data.latestExecutionId
    : null
  const batchLive = Boolean(batchExecutionId && currentExecutionId && !jobDone)
  const batchProgress = useBatchProgress(databaseId, batchExecutionId, batchLive)
  const save = useMutation({
    mutationFn: (input: DraftInput) => create
      ? createMigration(databaseId, input)
      : updateMigration(databaseId, migrationId, input),
    onSuccess: async (entry) => {
      setLiveSql(null)
      setDraftDirty(false)
      await queryClient.invalidateQueries({ queryKey: migrationQueryKeys.all(databaseId) })
      await navigate(`/databases/${databaseId}/migrations/${entry.id}`, { replace: create })
    },
  })
  const execute = useMutation({
    mutationFn: (action: ExecutionAction) => {
      return enqueueMigrationAction(databaseId, migrationId, action)
    },
    // 202: job masuk antrean. Status akhir dipantau lewat polling, bukan dari response ini.
    onSuccess: async (queued) => {
      const detailKey = migrationQueryKeys.detail(databaseId, migrationId)
      await queryClient.cancelQueries({ queryKey: detailKey, exact: true })
      queryClient.setQueryData<MigrationEntry>(detailKey,
        (entry) => entry ? { ...entry, latestJobId: queued.jobId } : entry)
    },
    // Ditolak gerbang approval (approval berubah sejak halaman dibuka): muat ulang status.
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: migrationQueryKeys.all(databaseId) })
    },
  })
  // Job selesai: segarkan migration, approval, dan riwayat; approval lama tidak berlaku untuk aksi berikutnya.
  useEffect(() => {
    if (!jobDone) return
    void queryClient.invalidateQueries({ queryKey: migrationQueryKeys.all(databaseId) })
    void queryClient.invalidateQueries({ queryKey: jobQueryKeys.history(databaseId) })
    void queryClient.invalidateQueries({ queryKey: ['migration-progress', databaseId] })
  }, [jobDone, databaseId, queryClient])
  const reconcile = useMutation({
    mutationFn: () => reconcileMigration(databaseId, migrationId, {
      inspectedTarget: true,
      note: reconciliationNote.trim(),
    }),
    onSuccess: (entry) => {
      queryClient.setQueryData(migrationQueryKeys.detail(databaseId, migrationId), entry)
      void queryClient.invalidateQueries({ queryKey: migrationQueryKeys.all(databaseId) })
    },
  })

  if (!create && detail.isPending) return <StatusMessage tone="loading">Memuat migration...</StatusMessage>
  if (!create && detail.isError) return <ErrorMessage error={detail.error} />
  if (create && database.isPending) return <StatusMessage tone="loading">Memuat database...</StatusMessage>
  if (create && database.isError) return <ErrorMessage error={database.error} />
  if (create && database.data?.accessLevel !== 'view_edit') return <StatusMessage tone="info">Akses View + Edit diperlukan untuk membuat migration.</StatusMessage>

  const entry = detail.data
  const editable = create || (entry?.canEdit === true && entry.status === 'draft')
  const jobActive = jobId !== null && !jobDone
  const busy = save.isPending || execute.isPending || reconcile.isPending || jobActive
  const currentSql = liveSql ?? entry?.sql ?? prefill?.sql ?? ''
  const databaseName = database.data?.name ?? 'database ini'
  const transactionalDdl = database.data?.capabilities?.transactionalDdl !== false
  // null = boleh dijalankan; selain itu = alasan tombol dikunci.
  const blockedReason = pending
    ? draftDirty ? 'Simpan perubahan draft sebelum menjalankan migration.' : executionBlockedReason(approval.data)
    : null

  function confirmExecution(action = pending) {
    if (!entry || !action) return
    const executionNote = entry.executionMode === 'batch'
      ? action.action === 'rollback'
        ? 'Down SQL dijalankan per batch dari yang terakhir. Jika gagal, batch down yang sudah commit tetap tercatat dan rollback dapat dilanjutkan setelah approval.'
        : 'Setiap batch commit terpisah. Jika berhenti, batch yang sudah commit tetap di target dan dapat dilanjutkan atau di-rollback setelah approval.'
      : transactionalDdl
        ? 'Dijalankan dalam satu transaksi; jika gagal semua perubahan dibatalkan.'
        : 'DML dijalankan dalam transaksi. DDL melakukan implicit commit; jika gagal, sebagian perubahan mungkin sudah diterapkan dan target perlu direkonsiliasi.'
    const confirmed = window.confirm(`${action.confirmTitle(entry.version, databaseName)}\n(${entry.name})\n\n${executionNote}\n${action.confirmNote}`)
    if (confirmed) execute.mutate(action.action)
  }

  return (
    <section>
      <div className="page-header">
        <div className="page-header__text">
          <Link className="migration-back" to={`/databases/${databaseId}/migrations`}>
            <Icon name="chevronLeft" size={14} />
            Riwayat migration
          </Link>
          <h1>
            {create ? 'Buat draft migration' : (
              <>
                <span className="migration-version migration-version--title">v{entry?.version}</span> {entry?.name}
              </>
            )}
          </h1>
          {entry ? (
            <div className="page-header__meta">
              <MigrationStatusBadge status={entry.status} />
              <span className="muted">revisi {entry.revision}</span>
              <span className="muted">dibuat oleh {entry.createdByUsername || 'Pengguna tidak tersedia'}</span>
            </div>
          ) : null}
        </div>
        {entry && pending && (entry.canEdit || pendingOptions.length > 1) ? (
          <div className="page-header__actions migration-run">
            {pendingOptions.length > 1 ? (
              <div aria-label="Pilih aksi untuk ditinjau" className="migration-run__choices" role="group">
                {pendingOptions.map((option) => (
                  <button
                    aria-pressed={pending.action === option.action}
                    className="button button--secondary"
                    key={option.action}
                    onClick={() => setSelectedAction(option.action)}
                    type="button"
                  >
                    Pilih: {option.label(entry.version)}
                  </button>
                ))}
              </div>
            ) : null}
            {entry.canEdit ? <button
              aria-describedby={blockedReason ? 'migration-run-reason' : undefined}
              className="button button--primary"
              disabled={busy || blockedReason !== null}
              onClick={() => confirmExecution()}
              type="button"
            >
              <Icon className={execute.isPending ? 'spinner' : undefined} name={execute.isPending ? 'loader' : blockedReason ? 'lock' : 'play'} size={16} />
              {execute.isPending ? pending.runningLabel : pending.label(entry.version)}
            </button> : null}
            {entry.canEdit && blockedReason ? (
              <p className="migration-run__reason" id="migration-run-reason">{blockedReason}</p>
            ) : null}
          </div>
        ) : null}
      </div>

      {jobId ? <JobStatusPanel error={job.isError ? job.error : null} job={job.data} loading={job.isPending} /> : null}
      {batchProgress.data ? (
        <BatchProgressPanel
          action={batchProgress.data.action ?? (currentExecutionId === batchExecutionId ? job.data?.action : undefined)}
          progress={batchProgress.data}
        />
      ) : null}
      {entry ? <MigrationResult entry={entry} failedAction={job.data?.status === 'failed' ? job.data.action : undefined} transactionalDdl={transactionalDdl} /> : null}
      {entry?.reconciledAt && entry.status !== 'reconciled' ? (
        <StatusMessage tone="info">
          Hasil transaksi dikonfirmasi melalui jurnal target pada {displayTime(entry.reconciledAt)}.
          {entry.reconciliationNote ? ` ${entry.reconciliationNote}` : ''}
        </StatusMessage>
      ) : null}
      {entry?.rollbackBlockedByVersion != null ? (
        <StatusMessage tone="info">
          Rollback v{entry.version} tertahan oleh v{entry.rollbackBlockedByVersion}. Selesaikan atau rollback versi tersebut lebih dulu.
        </StatusMessage>
      ) : null}
      {entry && !pending && entry.isActive !== false && (entry.status === 'succeeded' || entry.status === 'partially_reverted') && entry.downSql == null ? (
        <StatusMessage tone="info">
          Migration ini tidak punya Down SQL, jadi tidak dapat di-rollback.
          {entry.irreversibleReason ? ` Alasan: ${entry.irreversibleReason}` : ''}
        </StatusMessage>
      ) : null}
      {save.isError ? <ErrorMessage error={save.error} /> : null}
      {execute.isError ? <ErrorMessage error={execute.error} /> : null}
      {reconcile.isError ? <ErrorMessage error={reconcile.error} /> : null}
      {entry?.canReconcile ? (
        <div className="migration-result migration-result--warning migration-reconciliation" role="region" aria-labelledby="reconciliation-title">
          <div>
            <strong id="reconciliation-title">Periksa target sebelum membuka blokir</strong>
            <p>
              Periksa schema dan data target, lalu catat kondisi yang ditemukan. Tindakan ini tidak menjalankan ulang SQL gagal;
              migration berikutnya harus menangani perubahan yang masih diperlukan.
            </p>
            <label className="migration-reconciliation__label" htmlFor="reconciliation-note">Catatan pemeriksaan</label>
            <textarea
              id="reconciliation-note"
              className="migration-reconciliation__note"
              maxLength={1000}
              minLength={10}
              onChange={(event) => setReconciliationNote(event.target.value)}
              placeholder="Contoh: bonus_current.items belum memiliki kolom label maupun index items_label_idx."
              value={reconciliationNote}
            />
            <label className="migration-reconciliation__confirm">
              <input checked={targetInspected} onChange={(event) => setTargetInspected(event.target.checked)} type="checkbox" />
              Saya sudah memeriksa kondisi database target.
            </label>
            <button
              className="button button--primary"
              disabled={busy || !targetInspected || reconciliationNote.trim().length < 10}
              onClick={() => {
                if (window.confirm('Catat rekonsiliasi dan izinkan migration berikutnya dijalankan? SQL migration ini tidak akan dijalankan ulang.')) {
                  reconcile.mutate()
                }
              }}
              type="button"
            >
              {reconcile.isPending ? 'Mencatat...' : 'Catat rekonsiliasi'}
            </button>
          </div>
        </div>
      ) : null}

      <div className="migration-layout">
        <div className="migration-main">
          <MigrationEditor
            key={entry ? `${entry.id}/${entry.revision}` : 'new'}
            disabled={!editable}
            initialValue={entry ? {
              name: entry.name,
              sql: entry.sql,
              downSql: entry.downSql ?? undefined,
              irreversibleReason: entry.irreversibleReason ?? undefined,
              executionMode: entry.executionMode,
              batchSelectSql: entry.batchSelectSql ?? undefined,
              batchSize: entry.batchSize ?? undefined,
              batchCursorStart: entry.batchCursorStart,
            } : prefill}
            onDirtyChange={setDraftDirty}
            onSqlChange={setLiveSql}
            onSubmit={(input) => save.mutate(input)}
            pending={busy}
            primary={create}
            submitLabel={create ? 'Simpan draft' : 'Simpan perubahan'}
            supportsBatch={database.data?.engine === 'postgresql'}
            transactionalDdl={transactionalDdl}
          />
          {/* Milestone 2: review setelah SQL, supaya leader membaca SQL dulu baru memutuskan. */}
          {entry && pending ? (
            <ApprovalPanel
              action={pending.approval}
              databaseId={databaseId}
              databaseName={databaseName}
              error={approval.isError ? approval.error : null}
              loading={approval.isPending}
              migrationId={migrationId}
              review={approval.data}
              sql={pending.action === 'rollback' ? (entry.downSql ?? '') : entry.sql}
              verifyBatchRecovery={entry.executionMode === 'batch' && entry.downSql != null}
            />
          ) : null}
          {editable ? <ExplainPanel databaseId={databaseId} sql={currentSql} /> : null}
        </div>
        <aside className="migration-aside">
          <div className="panel">
            <h3>Aturan</h3>
            <ul className="migration-rules">
              <li>
                <Icon name="check" size={14} />
              {entry?.executionMode === 'batch' ? 'Setiap batch memiliki transaksi sendiri' : transactionalDdl ? 'Dijalankan dalam satu transaksi' : 'DML transaksional; DDL dapat melakukan implicit commit'}
              </li>
              <li>
                <Icon name="close" size={14} />
                BEGIN / COMMIT / ROLLBACK tidak diizinkan
              </li>
              {!transactionalDdl ? (
                <li>
                  <Icon name="alert" size={14} />
                  DDL terpisah dari DML; jika gagal, target mungkin perlu direkonsiliasi
                </li>
              ) : null}
              <li>
                <Icon name="info" size={14} />
                Maksimal 256 KiB SQL
              </li>
            </ul>
          </div>
          {entry ? (
            <div className="panel">
              <h3>Timeline</h3>
              <ol className="migration-timeline">
                <li className="is-done">
                  <strong>Dibuat</strong>
                  <span>{displayTime(entry.createdAt)}</span>
                </li>
                <li className={entry.startedAt ? 'is-done' : undefined}>
                  <strong>Dimulai</strong>
                  <span>{displayTime(entry.startedAt)}</span>
                  {entry.executedBy ? <span>oleh {entry.executedByUsername || 'Pengguna tidak tersedia'}</span> : null}
                </li>
                <li className={entry.finishedAt ? 'is-done' : undefined}>
                  <strong>Selesai</strong>
                  <span>{displayTime(entry.finishedAt)}</span>
                </li>
              </ol>
            </div>
          ) : null}
        </aside>
      </div>
    </section>
  )
}

// Panel hasil execution bersama Senja (konfeti, putar balik jam, kabel lepas).
function MigrationResult({ entry, failedAction, transactionalDdl }: { entry: MigrationEntry; failedAction?: ExecutionAction; transactionalDdl: boolean }) {
  if (entry.status === 'reconciled') {
    return (
      <div className="migration-result migration-result--warning" role="status">
        <img alt="" src={senja.unplugged} />
        <div>
          <strong>Kondisi target sudah diperiksa</strong>
          <p>
            Status asal: {entry.reconciledFrom === 'needs_reconciliation' ? 'perlu rekonsiliasi' : 'terputus'}.
            Ini bukan berarti SQL migration berhasil atau dijalankan ulang.
          </p>
          <p>{entry.reconciliationNote}</p>
          <small>
            Direkonsiliasi oleh {entry.reconciledByUsername || 'Pengguna tidak tersedia'} pada {displayTime(entry.reconciledAt)}.
          </small>
        </div>
      </div>
    )
  }
  if (entry.status === 'succeeded') {
    if (failedAction === 'rollback') {
      return <StatusMessage tone="info">Migration masih aktif karena rollback terakhir gagal.</StatusMessage>
    }
    return (
      <div className="migration-result migration-result--success" role="status">
        <img alt="" src={senja.success} />
        <div>
          <strong>Migration berhasil dijalankan</strong>
          <p>
            {entry.executionMode === 'batch'
              ? `Semua batch v${entry.version} sudah diterapkan.`
              : transactionalDdl
              ? `Semua perubahan v${entry.version} sudah diterapkan dalam satu transaksi.`
              : `Semua statement v${entry.version} berhasil diterapkan. DML bersifat transaksional; DDL MySQL memakai implicit commit.`}
          </p>
        </div>
      </div>
    )
  }
  if (entry.status === 'failed') {
    return (
      <div className="migration-result migration-result--danger" role="alert">
        <img alt="" src={senja.rewind} />
        <div>
          <strong>Migration gagal, semua perubahan di-rollback</strong>
          {entry.errorMessage ? <pre>{entry.errorMessage}</pre> : null}
        </div>
      </div>
    )
  }
  if (entry.status === 'interrupted') {
    return (
      <div className="migration-result migration-result--warning" role="alert">
        <img alt="" src={senja.unplugged} />
        <div>
          <strong>Hasil execution tidak pasti</strong>
          <p>Execution berikutnya diblokir sampai kondisi target direkonsiliasi. Hubungi admin.</p>
          {entry.errorMessage ? <pre>{entry.errorMessage}</pre> : null}
        </div>
      </div>
    )
  }
  if (entry.status === 'needs_reconciliation') {
    return (
      <div className="migration-result migration-result--warning" role="alert">
        <img alt="" src={senja.unplugged} />
        <div>
          <strong>Target perlu direkonsiliasi</strong>
          <p>Migration berikutnya diblokir sampai kondisi target diperiksa dan hasil pemeriksaan dicatat.</p>
          {entry.errorMessage ? <pre>{entry.errorMessage}</pre> : null}
        </div>
      </div>
    )
  }
  if (entry.status === 'rolled_back') {
    return (
      <div className="migration-result migration-result--warning" role="status">
        <img alt="" src={senja.rewind} />
        <div>
          <strong>Migration sudah di-rollback</strong>
          <p>{failedAction === 'forward' ? 'Forward terakhir gagal. Periksa penyebab kegagalannya sebelum mencoba lagi.' : `Perubahan v${entry.version} dibatalkan lewat Down SQL. Jalankan ulang membutuhkan approval forward.`}</p>
        </div>
      </div>
    )
  }
  if (entry.status === 'partial' || entry.status === 'partially_reverted') {
    const reverted = entry.status === 'partially_reverted'
    return (
      <div className="migration-result migration-result--warning" role="alert">
        <img alt="" src={senja.unplugged} />
        <div>
          <strong>{reverted ? 'Rollback berhenti di tengah' : 'Migration batch berhenti di tengah'}</strong>
          <p>
            {reverted
              ? 'Sebagian batch sudah dibatalkan. Lanjutkan rollback dari checkpoint terakhir setelah approval lengkap.'
              : 'Batch yang sudah committed tetap tersimpan. Lanjutkan dari checkpoint terakhir atau pilih rollback bila Down SQL tersedia.'}
          </p>
          {entry.errorMessage ? <pre>{entry.errorMessage}</pre> : null}
        </div>
      </div>
    )
  }
  if (entry.status === 'running') {
    return (
      <StatusMessage tone="loading" title="Sedang berjalan">
        Worker sedang memproses migration. Kamu bisa kembali ke halaman ini untuk memeriksa hasilnya.
      </StatusMessage>
    )
  }
  return null
}

interface MigrationPrefill {
  name: string
  sql: string
}

function readPrefill(state: unknown): MigrationPrefill | undefined {
  if (!state || typeof state !== 'object' || !('prefill' in state)) {
    return undefined
  }
  const prefill = (state as { prefill?: Partial<MigrationPrefill> }).prefill
  return typeof prefill?.name === 'string' && typeof prefill.sql === 'string'
    ? { name: prefill.name, sql: prefill.sql }
    : undefined
}
