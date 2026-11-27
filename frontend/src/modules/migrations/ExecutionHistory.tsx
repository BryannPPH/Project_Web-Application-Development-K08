import { useQuery } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { Link } from 'react-router-dom'

import { describeError } from '@/components/error-message'
import { StatusMessage } from '@/components/StatusMessage'
import { listExecutionHistory } from '@/modules/migrations/api'
import { jobQueryKeys } from '@/modules/migrations/jobs'
import { displayTime, executionActionText, executionStatusText } from '@/modules/migrations/query-helpers'
import type { MigrationEntry } from '@/modules/migrations/types'

const PAGE_SIZE = 100

export function ExecutionHistory({ databaseId, migrations }: { databaseId: string; migrations: MigrationEntry[] }) {
  const checkboxId = useId()
  const [hideRollback, setHideRollback] = useState(false)
  const [offset, setOffset] = useState(0)
  const history = useQuery({
    queryKey: jobQueryKeys.historyPage(databaseId, offset, hideRollback),
    queryFn: ({ signal }) => listExecutionHistory(databaseId, { limit: PAGE_SIZE, offset, hideRollback }, signal),
    enabled: databaseId !== '',
  })
  const byMigration = new Map(migrations.map((entry) => [entry.id, entry]))
  const shown = history.data?.items ?? []
  const total = history.data?.total ?? 0
  const pageNumber = Math.floor(offset / PAGE_SIZE) + 1
  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const pageOutOfRange = Boolean(history.data && offset > 0 && offset >= total)
  const lastValidOffset = total === 0 ? 0 : Math.floor((total - 1) / PAGE_SIZE) * PAGE_SIZE

  return (
    <section aria-labelledby={`${checkboxId}-title`} className="execution-history">
      <div className="execution-history__head">
        <h2 id={`${checkboxId}-title`}>Riwayat execution</h2>
        <label className="execution-history__toggle" htmlFor={checkboxId}>
          <input
            checked={hideRollback}
            id={checkboxId}
            onChange={(event) => {
              setHideRollback(event.target.checked)
              setOffset(0)
            }}
            type="checkbox"
          />
          Hide rollback
        </label>
      </div>
      <p className="muted execution-history__lead">
        Setiap percobaan tercatat sendiri. Hide rollback hanya menyembunyikan execution yang sudah dibatalkan beserta rollback
        suksesnya; forward dan percobaan yang gagal tetap tampil. Data tidak dihapus.
      </p>

      {history.isPending ? <StatusMessage tone="loading">Memuat riwayat execution...</StatusMessage> : null}
      {history.isError ? <StatusMessage tone="error">{describeError(history.error)}</StatusMessage> : null}
      {history.data && total === 0 ? (
        <StatusMessage tone="empty">{hideRollback ? 'Tidak ada execution yang cocok dengan filter ini.' : 'Belum ada execution untuk database ini.'}</StatusMessage>
      ) : null}
      {pageOutOfRange ? (
        <div className="execution-history__page-reset">
          <StatusMessage tone="empty">Jumlah riwayat berubah sehingga halaman ini tidak tersedia.</StatusMessage>
          <button className="button button--secondary" onClick={() => setOffset(lastValidOffset)} type="button">
            Muat halaman terakhir
          </button>
        </div>
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
                const name = item.migrationName || byMigration.get(item.migrationId)?.name
                return (
                  <tr key={item.executionId}>
                    <td data-label="Waktu">{displayTime(item.requestedAt)}</td>
                    <td data-label="Versi">
                      <Link className="migration-name-link" to={`/databases/${databaseId}/migrations/executions/${item.executionId}`}>
                        v{item.version}{name ? ` · ${name}` : ''}
                      </Link>
                    </td>
                    <td data-label="Aksi">{executionActionText[item.action]}</td>
                    <td data-label="Status">{executionStatusText[item.status] ?? item.status}</td>
                    <td data-label="Oleh">{item.actorUsername}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      ) : null}
      {history.data && total > 0 && !pageOutOfRange ? (
        <nav aria-label="Pagination riwayat execution" className="execution-history__pagination">
          <span className="muted">Menampilkan {offset + 1}–{offset + shown.length} dari {total} execution · halaman {pageNumber} dari {pageCount}</span>
          <div>
            <button className="button button--secondary" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))} type="button">
              Sebelumnya
            </button>
            <button className="button button--secondary" disabled={offset + PAGE_SIZE >= total} onClick={() => setOffset(offset + PAGE_SIZE)} type="button">
              Berikutnya
            </button>
          </div>
        </nav>
      ) : null}
    </section>
  )
}
