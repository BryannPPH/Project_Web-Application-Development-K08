/*
 * FILE INI UNTUK APA (DatabaseRows.tsx)
 * Kartu database — dipakai di Dashboard dan halaman /databases agar "membuka
 * database" (tugas paling sering) tampil sama di mana pun.
 * Setiap kartu menjelaskan level akses dengan kalimat, bukan hanya badge,
 * dan aksinya jelas: Buka viewer (utama) + Riwayat/Kelola migration.
 */
import { Link } from 'react-router-dom'

import { AccessBadge, EngineBadge } from '@/components/Badges'
import { Icon } from '@/components/Icon'
import type { AccessibleDatabase } from '@/modules/databases/types'
import '@/modules/databases/databases.css'

export function DatabaseRows({ databases }: { databases: AccessibleDatabase[] }) {
  return (
    <ul className="db-cards">
      {databases.map((database) => {
        const canEdit = database.accessLevel === 'view_edit'
        return (
          <li className={`db-card db-card--${database.engine}`} key={database.id}>
            <div className="db-card__head">
              <span aria-hidden="true" className="db-card__icon">
                <Icon name="database" size={22} />
              </span>
              <EngineBadge engine={database.engine} />
            </div>
            <div className="db-card__title">
              <Link className="db-card__name" to={`/databases/${database.id}`}>
                {database.name}
              </Link>
              <code className="db-card__dbname" translate="no">
                {database.databaseName}
              </code>
            </div>
            <p className="db-card__access">
              <AccessBadge level={database.accessLevel} />
              <span>{canEdit ? 'Bisa membaca & menjalankan migration' : 'Bisa membaca struktur & data'}</span>
            </p>
            <div className="db-card__actions">
              <Link className="button button--primary button--small" to={`/databases/${database.id}`}>
                <Icon name="table" size={14} />
                Buka viewer
              </Link>
              <Link className="button button--ghost button--small" to={`/databases/${database.id}/migrations`}>
                <Icon name="history" size={14} />
                {canEdit ? 'Kelola migration' : 'Riwayat migration'}
              </Link>
            </div>
          </li>
        )
      })}
    </ul>
  )
}

// Kerangka kartu saat memuat: ukuran sama dengan kartu asli agar tidak melompat.
export function DatabaseRowsSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <ul aria-hidden="true" className="db-cards">
      {Array.from({ length: rows }, (_, key) => (
        <li className="db-card db-card--skeleton" key={key}>
          <span className="skeleton-block" style={{ width: '2.75rem', height: '2.75rem' }} />
          <span className="skeleton-line" style={{ width: '60%' }} />
          <span className="skeleton-line" style={{ width: '35%' }} />
        </li>
      ))}
    </ul>
  )
}
