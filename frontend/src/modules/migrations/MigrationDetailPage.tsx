import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import { MigrationStatusBadge } from '@/components/Badges'
import { senja } from '@/components/brand'
import { Icon } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { ApiError } from '@/lib/api-client'
import { useAccessibleDatabase } from '@/modules/databases/api'
import { createMigration, executeMigration, getMigration, updateMigration } from '@/modules/migrations/api'
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

  if (!create && detail.isPending) return <StatusMessage tone="loading">Memuat migration…</StatusMessage>
  if (!create && detail.isError) return <ErrorMessage error={detail.error} />

  const entry = detail.data
  const editable = create || (entry?.canEdit === true && entry.status === 'draft')
  const busy = save.isPending || execute.isPending
  const currentSql = liveSql ?? entry?.sql ?? prefill?.sql ?? ''
  const databaseName = database.data?.name ?? 'database ini'
  const transactionalDdl = database.data?.capabilities?.transactionalDdl !== false

  function confirmExecution() {
    if (!entry) return
    const confirmed = window.confirm(
      `Jalankan v${entry.version} (${entry.name}) pada ${databaseName}?\n\nDijalankan dalam satu transaksi; jika gagal semua perubahan dibatalkan. Draft tidak dapat diedit lagi.`,
    )
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
              <span className="muted">dibuat oleh {entry.createdBy}</span>
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

      {entry ? <MigrationResult entry={entry} /> : null}
      {!transactionalDdl && editable ? (
        <StatusMessage title="DDL tidak didukung pada target ini" tone="warning">
          Target ini tidak mendukung DDL atomik. Migration hanya dapat berisi SELECT, INSERT, UPDATE, dan DELETE.
        </StatusMessage>
      ) : null}
      {save.isError ? <ErrorMessage error={save.error} /> : null}
      {execute.isError ? <ErrorMessage error={execute.error} /> : null}

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
                Dijalankan dalam satu transaksi
              </li>
              <li>
                <Icon name="close" size={14} />
                BEGIN / COMMIT / ROLLBACK tidak diizinkan
              </li>
              {!transactionalDdl ? (
                <li>
                  <Icon name="alert" size={14} />
                  Hanya DML: DDL (CREATE/ALTER/DROP) ditolak
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
                  {entry.executedBy ? <span>oleh {entry.executedBy}</span> : null}
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
function MigrationResult({ entry }: { entry: MigrationEntry }) {
  if (entry.status === 'succeeded') {
    return (
      <div className="migration-result migration-result--success" role="status">
        <img alt="" src={senja.success} />
        <div>
          <strong>Migration berhasil dijalankan</strong>
          <p>Semua perubahan v{entry.version} sudah diterapkan dalam satu transaksi.</p>
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
          <strong>Execution terputus</strong>
          <p>Execution berikutnya diblokir sampai kondisi target direkonsiliasi secara manual. Hubungi admin.</p>
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
