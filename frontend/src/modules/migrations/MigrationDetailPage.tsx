import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { ApiError } from '@/lib/api-client'
import { useAccessibleDatabase } from '@/modules/databases/api'
import { createMigration, executeMigration, getMigration, updateMigration } from '@/modules/migrations/api'
import { MigrationEditor } from '@/modules/migrations/MigrationEditor'
import { migrationQueryKeys, migrationStatusLabel } from '@/modules/migrations/query-helpers'
import type { DraftInput } from '@/modules/migrations/types'
import '@/modules/migrations/migrations.css'

function ErrorMessage({ error }: { error: unknown }) {
  return (
    <p className="migration-alert" role="alert">
      {error instanceof ApiError ? error.message : 'Permintaan migration gagal diproses.'}
    </p>
  )
}

export function MigrationDetailPage({ create = false }: { create?: boolean }) {
  const { databaseId = '', migrationId = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
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

  if (!create && detail.isPending) return <main className="migration-shell"><p>Memuat migration…</p></main>
  if (!create && detail.isError) return <main className="migration-shell"><ErrorMessage error={detail.error} /></main>

  const entry = detail.data
  const editable = create || (entry?.canEdit === true && entry.status === 'draft')
  const busy = save.isPending || execute.isPending

  function confirmExecution() {
    if (!entry) return
    const confirmed = window.confirm(
      `Jalankan migration v${entry.version} pada database ${databaseId}? SQL yang didukung akan dijalankan dalam satu transaction dan draft tidak dapat diedit lagi.`,
    )
    if (confirmed) execute.mutate()
  }

  return (
    <main className="migration-shell">
      <nav className="migration-breadcrumb" aria-label="Breadcrumb">
        <Link to={`/databases/${databaseId}/migrations`}>Riwayat migration</Link><span>/</span><span>{create ? 'Draft baru' : `v${entry?.version}`}</span>
      </nav>
      <div className="migration-heading">
        <div>
          <p className="migration-eyebrow">Database {databaseId}</p>
          <h1>{create ? 'Buat draft migration' : entry?.name}</h1>
          {entry ? <span className={`migration-status migration-status--${entry.status}`}>{migrationStatusLabel(entry.status)}</span> : null}
        </div>
        {entry?.canEdit && entry.status === 'draft' ? (
          <button className="button migration-execute" disabled={busy} onClick={confirmExecution} type="button">
            {execute.isPending ? 'Menjalankan…' : 'Execute migration'}
          </button>
        ) : null}
      </div>

      {entry?.errorMessage ? <p className="migration-alert" role="alert">{entry.errorMessage}</p> : null}
      {database.data?.capabilities?.transactionalDdl === false ? <p className="migration-alert">Target ini tidak mendukung DDL atomik. Migration hanya dapat berisi SELECT, INSERT, UPDATE, dan DELETE.</p> : null}
      {entry?.status === 'interrupted' ? <p className="migration-alert" role="alert">Execution berikutnya diblokir sampai kondisi target direkonsiliasi secara manual.</p> : null}
      {save.isError ? <ErrorMessage error={save.error} /> : null}
      {execute.isError ? <ErrorMessage error={execute.error} /> : null}

      <MigrationEditor
        disabled={!editable}
        transactionalDdl={database.data?.capabilities?.transactionalDdl}
        initialValue={entry ? { name: entry.name, sql: entry.sql } : undefined}
        onSubmit={(input) => save.mutate(input)}
        pending={busy}
        submitLabel={create ? 'Simpan draft' : 'Simpan perubahan'}
      />
    </main>
  )
}
