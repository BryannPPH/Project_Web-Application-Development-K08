import { useQuery } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { Link } from 'react-router-dom'

import { describeError } from '@/components/error-message'
import { StatusMessage } from '@/components/StatusMessage'
import { listExecutionHistory } from '@/modules/migrations/api'
import { hideRolledBack } from '@/modules/migrations/history'
import { jobQueryKeys } from '@/modules/migrations/jobs'
import { displayTime } from '@/modules/migrations/query-helpers'
import type { ExecutionAction, MigrationEntry } from '@/modules/migrations/types'

const actionText: Record<ExecutionAction, string> = {
  apply: 'Eksekusi',
  retry: 'Eksekusi ulang',
  rollback: 'Rollback',
  forward: 'Forward',
}

const statusText: Record<string, string> = {
  running: 'Berjalan',
  succeeded: 'Berhasil',
  failed: 'Gagal',
  partial: 'Sebagian',
  partially_reverted: 'Sebagian dibatalkan',
  interrupted: 'Terputus',
  needs_reconciliation: 'Perlu rekonsiliasi',
}

export function ExecutionHistory({ databaseId, migrations }: { databaseId: string; migrations: MigrationEntry[] }) {
  const checkboxId = useId()
  const [hideRollback, setHideRollback] = useState(false)
  const history = useQuery({
    queryKey: jobQueryKeys.history(databaseId),
    queryFn: ({ signal }) => listExecutionHistory(databaseId, signal),
    enabled: databaseId !== '',
  })
  const byMigration = new Map(migrations.map((entry) => [entry.id, entry]))
  const all = history.data?.items ?? []
  const shown = hideRollback ? hideRolledBack(all) : all
  const hiddenCount = all.length - shown.length

  return (
    <section aria-labelledby={`${checkboxId}-title`} className="execution-history">
      <div className="execution-history__head">
        <h2 id={`${checkboxId}-title`}>Riwayat execution</h2>
        <label className="execution-history__toggle" htmlFor={checkboxId}>
          <input checked={hideRollback} id={checkboxId} onChange={(event) => setHideRollback(event.target.checked)} type="checkbox" />
          Hide rollback
        </label>
      </div>
      <p className="muted execution-history__lead">
        Setiap percobaan tercatat sendiri. Hide rollback hanya menyembunyikan execution yang sudah dibatalkan beserta rollback
        suksesnya; forward dan percobaan yang gagal tetap tampil. Data tidak dihapus.
      </p>

      {history.isPending ? <StatusMessage tone="loading">Memuat riwayat execution...</StatusMessage> : null}
      {history.isError ? <StatusMessage tone="error">{describeError(history.error)}</StatusMessage> : null}
      {history.data && all.length === 0 ? <StatusMessage tone="empty">Belum ada execution untuk database ini.</StatusMessage> : null}
      {hideRollback && hiddenCount > 0 ? (
        <p aria-live="polite" className="muted execution-history__hidden">
          {hiddenCount} execution disembunyikan.
        </p>
      ) : null}
      {shown.length > 0 ? (
        <div className="data-table-wrapper">
          <table className="data-table data-table--stack execution-table">
            <thead>
              <tr>
                <th>Waktu</th>
                <th>Versi</th>
                <th>Aksi</th>
                <th>Status</th>
                <th>Oleh</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((item) => {
                const entry = byMigration.get(item.migrationId)
                return (
                  <tr key={item.executionId}>
                    <td data-label="Waktu">{displayTime(item.requestedAt)}</td>
                    <td data-label="Versi">
                      <Link className="migration-name-link" to={item.migrationId}>
                        v{item.version}{entry ? ` · ${entry.name}` : ''}
                      </Link>
                    </td>
                    <td data-label="Aksi">{actionText[item.action]}</td>
                    <td data-label="Status">{statusText[item.status] ?? item.status}</td>
                    <td data-label="Oleh">{item.actorUsername}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      ) : null}
      {hideRollback && all.length > 0 && shown.length === 0 ? (
        <StatusMessage tone="empty">Semua execution di halaman ini sudah dibatalkan. Matikan Hide rollback untuk melihatnya.</StatusMessage>
      ) : null}
    </section>
  )
}
