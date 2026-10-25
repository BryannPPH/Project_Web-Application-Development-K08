import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'

import { MigrationStatusBadge } from '@/components/Badges'
import { Icon } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { ApiError } from '@/lib/api-client'
import { listMigrations } from '@/modules/migrations/api'
import { displayTime, migrationQueryKeys } from '@/modules/migrations/query-helpers'
import '@/modules/migrations/migrations.css'

export function MigrationListPage() {
  const { databaseId = '' } = useParams()
  const query = useQuery({
    queryKey: migrationQueryKeys.all(databaseId),
    queryFn: ({ signal }) => listMigrations(databaseId, signal),
    enabled: databaseId !== '',
  })
  const interrupted = query.data?.items.some((entry) => entry.status === 'interrupted')

  return (
    <section>
      <div className="page-header">
        <div className="page-header__text">
          <h1>Riwayat migration</h1>
          <p className="muted">Versi linear terbaru ditampilkan lebih dulu. Setiap migration berjalan dalam satu transaksi.</p>
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
      {interrupted ? (
        <StatusMessage title="Execution diblokir" tone="warning">
          Ada migration berstatus Terputus. Hubungi admin untuk merekonsiliasi kondisi database sebelum menjalankan migration baru.
        </StatusMessage>
      ) : null}

      {query.isPending ? <StatusMessage tone="loading">Memuat riwayat…</StatusMessage> : null}
      {query.isError ? (
        <StatusMessage tone="error">
          {query.error instanceof ApiError ? query.error.message : 'Riwayat migration gagal dimuat.'}
        </StatusMessage>
      ) : null}
      {query.data && query.data.items.length === 0 ? (
        <StatusMessage illustration="lantern" title="Belum ada migration" tone="empty">
          Riwayat untuk database ini masih kosong.
        </StatusMessage>
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
    </section>
  )
}
