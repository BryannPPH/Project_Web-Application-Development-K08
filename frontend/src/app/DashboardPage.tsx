import { Link } from 'react-router-dom'

import { RoleBadge } from '@/components/Badges'
import { Icon, type IconName } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { useAdminGroups, useAdminUsers, useManagedDatabases } from '@/modules/admin/api'
import { useCurrentUser } from '@/modules/auth/use-current-user'
import { useAccessibleDatabases } from '@/modules/databases/api'

export function DashboardPage() {
  const currentUser = useCurrentUser()
  const databases = useAccessibleDatabases()
  const user = currentUser.data

  if (!user) {
    return null
  }

  const isAdmin = user.platformRole === 'platform_admin'
  const list = databases.data ?? []
  const editable = list.filter((database) => database.accessLevel === 'view_edit').length

  return (
    <section>
      <div className="page-header">
        <div className="page-header__text">
          <h1>Halo, {user.username}</h1>
          <div className="page-header__meta">
            <RoleBadge role={user.platformRole} />
            <span className="muted">
              {isAdmin ? 'Administrator platform' : 'Pengguna'}
            </span>
          </div>
        </div>
        <div className="page-header__actions">
          <Link className="button button--primary" to="/databases">
            <Icon name="database" size={16} />
            Buka database
          </Link>
          {isAdmin ? (
            <Link className="button button--secondary" to="/admin">
              <Icon name="shield" size={16} />
              Administrasi platform
            </Link>
          ) : null}
        </div>
      </div>

      <h2 className="section-title">Akses saya</h2>
      {databases.isPending ? <StatusMessage tone="loading">Memuat ringkasan akses…</StatusMessage> : null}
      {databases.isSuccess && list.length === 0 ? (
        <StatusMessage illustration="emptyBox" title="Belum ada akses database" tone="empty">
          Anda belum punya akses database. Minta admin menambahkan Anda ke group.
        </StatusMessage>
      ) : null}
      {databases.isSuccess && list.length > 0 ? (
        <div className="card-grid">
          <StatCard icon="database" label="Database tersedia" value={list.length} />
          <StatCard icon="pencil" label="Bisa diubah (View + Edit)" value={editable} />
          <StatCard icon="eye" label="Hanya lihat (View Only)" value={list.length - editable} />
        </div>
      ) : null}

      {isAdmin ? <AdminSummary /> : null}
    </section>
  )
}

function AdminSummary() {
  const users = useAdminUsers()
  const groups = useAdminGroups()
  const connections = useManagedDatabases()

  return (
    <>
      <h2 className="section-title">Platform</h2>
      <div className="card-grid">
        <StatCard
          action={{ label: 'Tambah user', to: '/admin/users' }}
          icon="user"
          label="User"
          value={users.data?.length}
        />
        <StatCard
          action={{ label: 'Kelola group', to: '/admin/groups' }}
          icon="users"
          label="Group"
          value={groups.data?.length}
        />
        <StatCard
          action={{ label: 'Tambah koneksi', to: '/admin/databases' }}
          icon="plug"
          label="Koneksi database"
          value={connections.data?.length}
        />
      </div>
    </>
  )
}

interface StatCardProps {
  icon: IconName
  label: string
  value: number | undefined
  action?: { label: string; to: string }
}

function StatCard({ icon, label, value, action }: StatCardProps) {
  return (
    <div className="stat-card">
      <span className="stat-card__label">
        <span className="stat-card__icon">
          <Icon name={icon} size={16} />
        </span>
        {label}
      </span>
      <span className="stat-card__value">{value ?? '–'}</span>
      {action ? (
        <div className="stat-card__foot">
          <Link className="button button--secondary button--small" to={action.to}>
            {action.label}
            <Icon name="arrowRight" size={14} />
          </Link>
        </div>
      ) : null}
    </div>
  )
}
