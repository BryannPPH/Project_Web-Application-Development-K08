import { useQuery } from '@tanstack/react-query'
import { Link, useParams, useSearchParams } from 'react-router-dom'

import { MigrationStatusBadge } from '@/components/Badges'
import { Icon } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { ApiError } from '@/lib/api-client'
import { ExecutionHistory } from '@/modules/migrations/ExecutionHistory'
import { listMigrations } from '@/modules/migrations/api'
import { displayTime, migrationQueryKeys } from '@/modules/migrations/query-helpers'
import '@/modules/migrations/migrations.css'

const PAGE_SIZE = 50

export function MigrationListPage() {
  const { databaseId = '' } = useParams()
  const [search, setSearch] = useSearchParams()
  const requestedPage = Number(search.get('page') ?? '1')
  const page = requestedPage > 0 && Number.isSafeInteger(requestedPage * PAGE_SIZE) && Number.isInteger(requestedPage) ? requestedPage : 1
  const offset = (page - 1) * PAGE_SIZE
  const query = useQuery({
    queryKey: migrationQueryKeys.list(databaseId, offset),
    queryFn: ({ signal }) => listMigrations(databaseId, { limit: PAGE_SIZE, offset }, signal),
    enabled: databaseId !== '',
  })
  const reconciliationRequired = query.data?.items.some(
    (entry) => entry.status === 'interrupted' || entry.status === 'needs_reconciliation',
  )
  function goToPage(nextPage: number) {
    setSearch((current) => {
      const updated = new URLSearchParams(current)
      if (nextPage === 1) updated.delete('page')
      else updated.set('page', String(nextPage))
      return updated
    })
  }

  return (
    <section>
      <div className="page-header">
        <div className="page-header__text">
          <h1>Riwayat migration</h1>
          <p className="muted">Versi linear terbaru ditampilkan lebih dulu. Status dan SQL tercatat di riwayat.</p>
        </div>
        <div className="page-header__actions">
          <Link className="button button--secondary" to={`/databases/${databaseId}`}>
            <Icon name="table" size={16} />
            Buka viewer
          </Link>
          <Link className="button button--secondary" to={`/databases/${databaseId}/schema-diff`}>
            <Icon name="compare" size={16} />
            Bandingkan schema
          </Link>
          {query.data?.canEdit ? (
            <Link className="button button--primary" to="new">
              <Icon name="plus" size={16} />
              Buat draft
            </Link>
          ) : null}
        </div>
      </div>

      {query.data && !query.data.canEdit ? (
        <StatusMessage tone="info">Anda memiliki akses View Only: riwayat dapat dibaca, tetapi tidak dapat membuat migration.</StatusMessage>
      ) : null}
      {reconciliationRequired ? (
        <StatusMessage title="Execution diblokir" tone="warning">
          Ada migration yang membutuhkan rekonsiliasi. Periksa kondisi database target dan hubungi admin sebelum menjalankan migration baru.
        </StatusMessage>
      ) : null}

      {query.isPending ? <StatusMessage tone="loading">Memuat riwayat...</StatusMessage> : null}
      {query.isError ? (
        <StatusMessage tone="error">
          {query.error instanceof ApiError ? query.error.message : 'Riwayat migration gagal dimuat.'}
        </StatusMessage>
      ) : null}
      {query.data && query.data.items.length === 0 && page === 1 ? (
        <StatusMessage illustration="lantern" title="Belum ada migration" tone="empty">
          Riwayat untuk database ini masih kosong.
        </StatusMessage>
      ) : null}
      {query.data && query.data.items.length === 0 && page > 1 ? (
        <div className="execution-history__page-reset">
          <StatusMessage tone="empty">Halaman versi ini tidak tersedia.</StatusMessage>
          <button className="button button--secondary" onClick={() => goToPage(1)} type="button">Kembali ke halaman pertama</button>
        </div>
      ) : null}
      {query.data && query.data.items.length > 0 ? (
        <div className="data-table-wrapper">
          <table className="data-table data-table--stack migration-table">
            <thead>
              <tr>
                <th>Versi</th>
                <th>Nama</th>
                <th>Status</th>
                <th>Dibuat</th>
                <th>Dieksekusi</th>
                <th>Aksi</th>
              </tr>
            </thead>
            <tbody>
              {query.data.items.map((entry) => (
                <tr key={entry.id}>
                  <td data-label="Versi">
                    <span className="migration-version">v{entry.version}</span>
                  </td>
                  <td data-label="Nama">
                    <Link className="migration-name-link" to={entry.id}>{entry.name}</Link>
                  </td>
                  <td data-label="Status">
                    <MigrationStatusBadge status={entry.status} />
                  </td>
                  <td data-label="Dibuat">{displayTime(entry.createdAt)}</td>
                  <td data-label="Dieksekusi">{displayTime(entry.finishedAt ?? entry.startedAt)}</td>
                  <td className="cell-actions" data-label="Aksi">
                    <Link className="button button--secondary button--small" to={entry.id}>
                      <Icon name="eye" size={14} />
                      Lihat detail
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {query.data && query.data.items.length > 0 && query.data.total > PAGE_SIZE ? (
        <nav aria-label="Pagination versi migration" className="execution-history__pagination">
          <span className="muted">Menampilkan {offset + 1}–{offset + query.data.items.length} dari {query.data.total} versi · halaman {page} dari {Math.ceil(query.data.total / PAGE_SIZE)}</span>
          <div>
            <button className="button button--secondary" disabled={page === 1} onClick={() => goToPage(page - 1)} type="button">Sebelumnya</button>
            <button className="button button--secondary" disabled={offset + PAGE_SIZE >= query.data.total} onClick={() => goToPage(page + 1)} type="button">Berikutnya</button>
          </div>
        </nav>
      ) : null}
      {query.data && (query.data.items.length > 0 || page > 1) ? (
        <ExecutionHistory databaseId={databaseId} key={databaseId} migrations={query.data.items} />
      ) : null}
    </section>
  )
}
