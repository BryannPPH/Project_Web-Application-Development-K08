import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import { MigrationStatusBadge } from '@/components/Badges'
import { senja } from '@/components/brand'
import { Icon } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { ApiError } from '@/lib/api-client'
import { useAccessibleDatabase } from '@/modules/databases/api'
import { createMigration, executeMigration, getMigration, reconcileMigration, updateMigration } from '@/modules/migrations/api'
import { ExplainPanel } from '@/modules/migrations/ExplainPanel'
import { MigrationEditor } from '@/modules/migrations/MigrationEditor'
import { displayTime, migrationQueryKeys } from '@/modules/migrations/query-helpers'
import type { DraftInput, MigrationEntry } from '@/modules/migrations/types'
import '@/modules/migrations/migrations.css'

function ErrorMessage({ error }: { error: unknown }) {
  return (
    <StatusMessage tone="error">
      {error instanceof ApiError ? error.message : 'Permintaan migration gagal diproses.'}
    </StatusMessage>
  )
}

export function MigrationDetailPage({ create = false }: { create?: boolean }) {
  const { databaseId = '', migrationId = '' } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  // Draft baru dapat diisi otomatis dari halaman schema diff.
  const prefill = create ? readPrefill(location.state) : undefined
  const [liveSql, setLiveSql] = useState<string | null>(null)
  const [reconciliationNote, setReconciliationNote] = useState('')
  const [targetInspected, setTargetInspected] = useState(false)
  const database = useAccessibleDatabase(databaseId)
  const detail = useQuery({
    queryKey: migrationQueryKeys.detail(databaseId, migrationId),
    queryFn: ({ signal }) => getMigration(databaseId, migrationId, signal),
    enabled: !create && databaseId !== '' && migrationId !== '',
  })
  const save = useMutation({
    mutationFn: (input: DraftInput) => create
      ? createMigration(databaseId, input)
      : updateMigration(databaseId, migrationId, input),
    onSuccess: async (entry) => {
      await queryClient.invalidateQueries({ queryKey: migrationQueryKeys.all(databaseId) })
      await navigate(`/databases/${databaseId}/migrations/${entry.id}`, { replace: create })
    },
  })
  const execute = useMutation({
    mutationFn: () => executeMigration(databaseId, migrationId),
    onSuccess: (entry) => {
      queryClient.setQueryData(migrationQueryKeys.detail(databaseId, migrationId), entry)
      void queryClient.invalidateQueries({ queryKey: migrationQueryKeys.all(databaseId) })
    },
  })
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

  if (!create && detail.isPending) return <StatusMessage tone="loading">Memuat migration…</StatusMessage>
  if (!create && detail.isError) return <ErrorMessage error={detail.error} />

  const entry = detail.data
  const editable = create || (entry?.canEdit === true && entry.status === 'draft')
  const busy = save.isPending || execute.isPending || reconcile.isPending
  const currentSql = liveSql ?? entry?.sql ?? prefill?.sql ?? ''
  const databaseName = database.data?.name ?? 'database ini'
  const transactionalDdl = database.data?.capabilities?.transactionalDdl !== false

  function confirmExecution() {
    if (!entry) return
    const executionNote = transactionalDdl
      ? 'Dijalankan dalam satu transaksi; jika gagal semua perubahan dibatalkan.'
      : 'DML dijalankan dalam transaksi. DDL melakukan implicit commit; jika gagal, sebagian perubahan mungkin sudah diterapkan dan target perlu direkonsiliasi.'
    const confirmed = window.confirm(`Jalankan v${entry.version} (${entry.name}) pada ${databaseName}?\n\n${executionNote}\nDraft tidak dapat diedit lagi.`)
    if (confirmed) execute.mutate()
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
              <span className="muted">dibuat oleh {entry.createdByUsername || 'Pengguna tidak tersedia'}</span>
            </div>
          ) : null}
        </div>
        {entry?.canEdit && entry.status === 'draft' ? (
          <div className="page-header__actions">
            <button className="button button--primary" disabled={busy} onClick={confirmExecution} type="button">
              <Icon className={execute.isPending ? 'spinner' : undefined} name={execute.isPending ? 'loader' : 'play'} size={16} />
              {execute.isPending ? 'Menjalankan…' : `Jalankan v${entry.version}`}
            </button>
          </div>
        ) : null}
      </div>

      {entry ? <MigrationResult entry={entry} transactionalDdl={transactionalDdl} /> : null}
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
              {reconcile.isPending ? 'Mencatat…' : 'Catat rekonsiliasi'}
            </button>
          </div>
        </div>
      ) : null}

      <div className="migration-layout">
        <div className="migration-main">
          <MigrationEditor
            disabled={!editable}
            initialValue={entry ? { name: entry.name, sql: entry.sql } : prefill}
            onSqlChange={setLiveSql}
            onSubmit={(input) => save.mutate(input)}
            pending={busy}
            submitLabel={create ? 'Simpan draft' : 'Simpan perubahan'}
            transactionalDdl={transactionalDdl}
          />
          {editable ? <ExplainPanel databaseId={databaseId} sql={currentSql} /> : null}
        </div>
        <aside className="migration-aside">
          <div className="panel">
            <h3>Aturan</h3>
            <ul className="migration-rules">
              <li>
                <Icon name="check" size={14} />
                {transactionalDdl ? 'Dijalankan dalam satu transaksi' : 'DML transaksional; DDL dapat melakukan implicit commit'}
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
function MigrationResult({ entry, transactionalDdl }: { entry: MigrationEntry; transactionalDdl: boolean }) {
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
    return (
      <div className="migration-result migration-result--success" role="status">
        <img alt="" src={senja.success} />
        <div>
          <strong>Migration berhasil dijalankan</strong>
          <p>
            {transactionalDdl
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
          <strong>Migration gagal — semua perubahan di-rollback</strong>
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
          <p>DDL MySQL dapat menyisakan perubahan sebelumnya. Migration berikutnya diblokir sampai kondisi target diperiksa.</p>
          {entry.errorMessage ? <pre>{entry.errorMessage}</pre> : null}
        </div>
      </div>
    )
  }
  if (entry.status === 'running') {
    return (
      <StatusMessage tone="loading" title="Sedang berjalan">
        Migration sedang dijalankan. Jangan tutup halaman ini.
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
