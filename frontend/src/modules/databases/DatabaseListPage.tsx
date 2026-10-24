import { useState } from 'react'
import { Link } from 'react-router-dom'

import { AccessBadge, EngineBadge } from '@/components/Badges'
import { describeError } from '@/components/error-message'
import { Icon } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { useCurrentUser } from '@/modules/auth/use-current-user'
import { useAccessibleDatabases } from '@/modules/databases/api'
import type { AccessLevel } from '@/modules/databases/types'
import '@/modules/databases/databases.css'

type AccessFilter = 'all' | AccessLevel

const accessFilters: { value: AccessFilter; label: string }[] = [
  { value: 'all', label: 'Semua' },
  { value: 'view_only', label: 'Hanya lihat' },
  { value: 'view_edit', label: 'Bisa ubah' },
]

export function DatabaseListPage() {
  const databases = useAccessibleDatabases()
  const currentUser = useCurrentUser()
  const [search, setSearch] = useState('')
  const [access, setAccess] = useState<AccessFilter>('all')

  const keyword = search.trim().toLowerCase()
  const visible =
    databases.data?.filter(
      (database) =>
        (access === 'all' || database.accessLevel === access) &&
        (keyword === '' ||
          database.name.toLowerCase().includes(keyword) ||
          database.databaseName.toLowerCase().includes(keyword)),
    ) ?? []

  return (
    <section>
      <div className="page-header">
        <div className="page-header__text">
          <h1>Database</h1>
          <p className="muted">
            Hanya database yang diberikan ke group Anda. View Only untuk membaca, View + Edit
            untuk menjalankan migration.
          </p>
        </div>
      </div>

      {databases.isPending ? (
        <div aria-hidden="true" className="db-grid">
          {[0, 1, 2].map((key) => (
            <div className="db-card db-card--skeleton" key={key} />
          ))}
        </div>
      ) : null}
      {databases.isPending ? <span className="visually-hidden" role="status">Memuat daftar database…</span> : null}

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
        <StatusMessage illustration="searching" title="Belum ada database" tone="empty">
          Belum ada database yang bisa Anda akses. Minta admin menambahkan Anda ke group.
          {currentUser.data?.platformRole === 'platform_admin' ? (
            <p className="db-empty-hint">
              Sebagai admin, Anda tetap perlu grant lewat group.{' '}
              <Link to="/admin/groups">Atur di Administrasi › Group &amp; akses</Link>
            </p>
          ) : null}
        </StatusMessage>
      ) : null}

      {databases.data && databases.data.length > 0 ? (
        <>
          <div className="toolbar">
            <label className="search-field">
              <Icon name="search" size={16} />
              <input
                aria-label="Cari database"
                className="input"
                onChange={(event) => setSearch(event.target.value)}
                placeholder="Cari nama database…"
                type="search"
                value={search}
              />
            </label>
            <div aria-label="Filter akses" className="segmented" role="group">
              {accessFilters.map((filter) => (
                <button
                  aria-pressed={access === filter.value}
                  key={filter.value}
                  onClick={() => setAccess(filter.value)}
                  type="button"
                >
                  {filter.label}
                </button>
              ))}
            </div>
          </div>

          {visible.length === 0 ? (
            <StatusMessage
              actionLabel="Hapus filter"
              illustration="searching"
              onAction={() => {
                setSearch('')
                setAccess('all')
              }}
              title="Database tidak ditemukan"
              tone="empty"
            >
              Tidak ada database yang cocok dengan filter.
            </StatusMessage>
          ) : (
            <ul className="db-grid">
              {visible.map((database) => (
                <li className="db-card" key={database.id}>
                  <div className="db-card__head">
                    <span className="db-card__icon">
                      <Icon name="database" size={20} />
                    </span>
                    <div className="db-card__title">
                      <h2>{database.name}</h2>
                      <code>{database.databaseName}</code>
                    </div>
                  </div>
                  <div className="db-card__badges">
                    <AccessBadge level={database.accessLevel} />
                    <EngineBadge engine={database.engine} />
                  </div>
                  <div className="db-card__actions">
                    <Link className="button button--primary button--small" to={`/databases/${database.id}`}>
                      Buka viewer
                    </Link>
                    <Link
                      className="button button--secondary button--small"
                      to={`/databases/${database.id}/migrations`}
                    >
                      Riwayat migration
                    </Link>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </>
      ) : null}
    </section>
  )
}
