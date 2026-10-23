import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'

import { ApiError } from '@/lib/api-client'
import { listMigrations } from '@/modules/migrations/api'
import { migrationQueryKeys, migrationStatusLabel } from '@/modules/migrations/query-helpers'
import '@/modules/migrations/migrations.css'

function displayTime(value: string | null) {
  return value
    ? new Intl.DateTimeFormat('id-ID', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
    : '—'
}

export function MigrationListPage() {
  const { databaseId = '' } = useParams()
  const query = useQuery({
    queryKey: migrationQueryKeys.all(databaseId),
    queryFn: ({ signal }) => listMigrations(databaseId, signal),
    enabled: databaseId !== '',
  })

  return (
    <main className="migration-shell">
      <nav className="migration-breadcrumb" aria-label="Breadcrumb">
        <Link to="/">Dashboard</Link><span>/</span><span>Migration</span>
      </nav>
      <div className="migration-heading">
        <div>
          <p className="migration-eyebrow">Database {databaseId}</p>
          <h1>Riwayat migration</h1>
          <p>Versi linear terbaru ditampilkan lebih dulu.</p>
        </div>
        {query.data?.canEdit ? (
          <Link className="button button--primary migration-link-button" to="new">Buat draft</Link>
        ) : null}
      </div>

      {query.isPending ? <p>Memuat riwayat…</p> : null}
      {query.isError ? (
        <p className="migration-alert" role="alert">
          {query.error instanceof ApiError ? query.error.message : 'Riwayat migration gagal dimuat.'}
        </p>
      ) : null}
      {query.data && query.data.items.length === 0 ? (
        <section className="migration-empty"><h2>Belum ada migration</h2><p>Riwayat untuk database ini masih kosong.</p></section>
      ) : null}
      {query.data && query.data.items.length > 0 ? (
        <div className="migration-table-wrap">
          <table className="migration-table">
            <thead><tr><th>Versi</th><th>Nama</th><th>Status</th><th>Dibuat</th><th>Dieksekusi</th></tr></thead>
            <tbody>
              {query.data.items.map((entry) => (
                <tr key={entry.id}>
                  <td>v{entry.version}</td>
                  <td><Link to={entry.id}>{entry.name}</Link></td>
                  <td><span className={`migration-status migration-status--${entry.status}`}>{migrationStatusLabel(entry.status)}</span></td>
                  <td>{displayTime(entry.createdAt)}</td>
                  <td>{displayTime(entry.finishedAt ?? entry.startedAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </main>
  )
}
