import { Icon, type IconName } from '@/components/Icon'
import type { PlatformRole } from '@/modules/auth/types'
import { accessLevelLabel, databaseEngineLabel, type AccessLevel, type DatabaseEngine } from '@/modules/databases/types'
import { migrationStatusLabel } from '@/modules/migrations/query-helpers'
import type { MigrationStatus } from '@/modules/migrations/types'

// Status tidak pernah hanya bergantung pada warna: selalu ikon + teks.
export function AccessBadge({ level }: { level: AccessLevel }) {
  const edit = level === 'view_edit'
  return (
    <span className={edit ? 'badge badge--edit' : 'badge badge--view'}>
      <Icon name={edit ? 'pencil' : 'eye'} size={12} />
      {accessLevelLabel(level)}
    </span>
  )
}

const statusStyles: Record<MigrationStatus, { className: string; icon: IconName }> = {
  draft: { className: 'badge', icon: 'pencil' },
  running: { className: 'badge badge--info', icon: 'loader' },
  succeeded: { className: 'badge badge--success', icon: 'check' },
  failed: { className: 'badge badge--danger', icon: 'error' },
  interrupted: { className: 'badge badge--warning', icon: 'alert' },
}

export function MigrationStatusBadge({ status }: { status: MigrationStatus }) {
  const style = statusStyles[status]
  return (
    <span className={style.className}>
      <Icon className={status === 'running' ? 'spinner' : undefined} name={style.icon} size={12} />
      {migrationStatusLabel(status)}
    </span>
  )
}

export function EngineBadge({ engine }: { engine: DatabaseEngine }) {
  return (
    <span className={`badge badge--engine badge--engine-${engine}`}>
      <Icon name="database" size={12} />
      {databaseEngineLabel(engine)}
    </span>
  )
}

export function ActiveBadge({ active }: { active: boolean }) {
  return (
    <span className={active ? 'badge badge--success' : 'badge'}>
      <Icon name={active ? 'check' : 'close'} size={12} />
      {active ? 'Aktif' : 'Nonaktif'}
    </span>
  )
}

export function RoleBadge({ role }: { role: PlatformRole }) {
  return role === 'platform_admin' ? (
    <span className="badge badge--admin">Admin</span>
  ) : (
    <span className="badge">User</span>
  )
}
