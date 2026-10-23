import { Link } from 'react-router-dom'

import { describeError } from '@/components/error-message'
import { StatusMessage } from '@/components/StatusMessage'
import { useAccessibleDatabases } from '@/modules/databases/api'
import { accessLevelLabel } from '@/modules/databases/types'

export function DatabaseListPage() {
  const databases = useAccessibleDatabases()

  return (
    <section>
      <div className="page-header">
        <h1>Database</h1>
        <span className="muted">Hanya database yang diberikan ke group Anda.</span>
      </div>

      {databases.isPending ? <StatusMessage>Memuat daftar database…</StatusMessage> : null}

      {databases.isError ? (
        <StatusMessage
          actionLabel="Coba lagi"
          onAction={() => void databases.refetch()}
          title="Daftar database gagal dimuat"
          tone="error"
        >
          {describeError(databases.error)}
        </StatusMessage>
      ) : null}

      {databases.data && databases.data.length === 0 ? (
        <StatusMessage title="Belum ada database" tone="empty">
          Anda belum tergabung dalam group yang memiliki akses database. Hubungi
          administrator platform.
        </StatusMessage>
      ) : null}

      {databases.data && databases.data.length > 0 ? (
        <div className="data-table-wrapper">
          <table className="data-table">
            <thead>
              <tr>
                <th>Nama</th>
                <th>Database</th>
                <th>Akses</th>
                <th aria-label="Aksi" />
              </tr>
            </thead>
            <tbody>
              {databases.data.map((database) => (
                <tr key={database.id}>
                  <td>{database.name}</td>
                  <td>
                    <code>{database.databaseName}</code>
                  </td>
                  <td>
                    <span
                      className={
                        database.accessLevel === 'view_edit'
                          ? 'badge badge--success'
                          : 'badge'
                      }
                    >
                      {accessLevelLabel(database.accessLevel)}
                    </span>
                  </td>
                  <td>
                    <div className="inline-actions">
                      <Link
                        className="button button--secondary button--small"
                        to={`/databases/${database.id}`}
                      >
                        Buka viewer
                      </Link>
                      <Link
                        className="button button--secondary button--small"
                        to={`/databases/${database.id}/migrations`}
                      >
                        Riwayat migration
                      </Link>
                    </div>
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
