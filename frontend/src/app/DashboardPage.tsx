import { useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import type { SenjaClipName } from '@/assets/brand/senja-rig/web/senja-data'
import { describeError } from '@/components/error-message'
import { CaveScene } from '@/components/CaveScene'
import { Icon, type IconName } from '@/components/Icon'
import { SenjaRig, type SenjaOutfit } from '@/components/SenjaRig'
import { StatusMessage } from '@/components/StatusMessage'
import { useAdminGroups, useAdminUsers, useManagedDatabases } from '@/modules/admin/api'
import { useCurrentUser } from '@/modules/auth/use-current-user'
import { useAccessibleDatabases } from '@/modules/databases/api'
import { DatabaseRows, DatabaseRowsSkeleton } from '@/modules/databases/DatabaseRows'
import '@/app/dashboard.css'

const DASHBOARD_LIMIT = 6

type DayPart = 'pagi' | 'siang' | 'sore' | 'malam'

function dayPart(hour: number): DayPart {
  if (hour >= 4 && hour < 11) return 'pagi'
  if (hour >= 11 && hour < 15) return 'siang'
  if (hour >= 15 && hour < 19) return 'sore'
  return 'malam'
}

// Senja per waktu: kostum (dan latar banner di dashboard.css) mengikuti waktu;
// intro diputar sekali saat dashboard dibuka, lalu loop; gesture saat banner disorot.
interface MascotPlan {
  intro?: SenjaClipName
  loop: SenjaClipName
  gesture?: SenjaClipName
  outfit: SenjaOutfit
}

const senjaByPart: Record<DayPart, MascotPlan> = {
  pagi: { intro: 'stretch', loop: 'idle', gesture: 'wave', outfit: 'pagi' },
  siang: { intro: 'wave', loop: 'idle', gesture: 'wave', outfit: 'siang' },
  sore: { loop: 'idle', gesture: 'wave', outfit: 'sore' },
  malam: { loop: 'sleepy', outfit: 'malam' },
}

const todayFormat = new Intl.DateTimeFormat('id-ID', { weekday: 'long', day: 'numeric', month: 'long' })

export function DashboardPage() {
  const currentUser = useCurrentUser()
  const databases = useAccessibleDatabases()
  const heroRef = useRef<HTMLElement>(null)
  // Waktu dibaca sekali saat dashboard dibuka (bukan di setiap render).
  const [now] = useState(() => new Date())
  const user = currentUser.data

  if (!user) {
    return null
  }

  const isAdmin = user.platformRole === 'platform_admin'
  const list = databases.data ?? []
  const editable = list.filter((database) => database.accessLevel === 'view_edit').length
  const part = dayPart(now.getHours())
  const mascot = senjaByPart[part]

  return (
    <section className="dashboard">
      <header className={`hero hero--${part}`} ref={heroRef}>
        <CaveScene className="hero__scene" />
        <div className="hero__text">
          <p className="hero__date">{todayFormat.format(now)}</p>
          <h1 className="hero__greeting">
            Selamat {part}, <span className="hero__name">{user.username}</span>
          </h1>
        </div>
        <SenjaRig
          className="hero__art"
          gesture={mascot.gesture}
          gestureTrigger={heroRef}
          intro={mascot.intro}
          loop={mascot.loop}
          outfit={mascot.outfit}
        />
      </header>

      <div className="dashboard__body">
      <StatsRail editable={editable} isAdmin={isAdmin} loaded={databases.isSuccess} total={list.length} />

      <div className="dashboard__main">
      <div className="section-head">
        <h2 className="section-head__title" data-area="database">
          <span aria-hidden="true" className="area-tile">
            <Icon name="database" size={16} />
          </span>
          Database Anda
        </h2>
        {list.length > DASHBOARD_LIMIT ? (
          <Link className="section-head__link" to="/databases">
            Lihat semua {list.length}
            <Icon name="arrowRight" size={14} />
          </Link>
        ) : null}
      </div>

      {databases.isPending ? <DatabaseRowsSkeleton rows={2} /> : null}
      {databases.isPending ? <span className="visually-hidden" role="status">Memuat database...</span> : null}

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

      {databases.isSuccess && list.length === 0 ? (
        <StatusMessage illustration="emptyBox" title="Belum ada akses database" tone="empty">
          Minta admin menambahkan Anda ke group yang punya akses database.
          {isAdmin ? (
            <p className="db-empty-hint">
              Role admin tidak otomatis memberi akses data.{' '}
              <Link to="/admin/groups">Beri akses lewat Group & akses</Link>
            </p>
          ) : null}
        </StatusMessage>
      ) : null}

      {list.length > 0 ? <DatabaseRows databases={list.slice(0, DASHBOARD_LIMIT)} /> : null}

      {isAdmin ? <AdminSummary /> : null}
      </div>
      </div>
    </section>
  )
}

interface StatsRailProps {
  editable: number
  isAdmin: boolean
  loaded: boolean
  total: number
}

// Rel kanan berisi ubin persegi berukuran kecil: angka yang paling sering dicari
// (berapa database di sistem, berapa yang bisa diakses, berapa yang bisa diubah).
// User, group, dan koneksi ada di bagian Administrasi.
function StatsRail({ editable, isAdmin, loaded, total }: StatsRailProps) {
  return (
    <aside aria-label="Ringkasan database" className="rail">
      {isAdmin ? (
        <AdminTiles editable={editable} loaded={loaded} total={total} />
      ) : (
        <>
          <StatTile label="Bisa Anda akses" note="database" tone="lilac" value={loaded ? total : undefined} />
          <StatTile
            label="Bisa diubah"
            note={`dari ${total}`}
            share={total > 0 ? editable / total : 0}
            tone="mint"
            value={loaded ? editable : undefined}
          />
        </>
      )}
    </aside>
  )
}

// AdminTiles: hook admin hanya dipanggil bila user admin (komponen ini hanya dirender untuk admin).
function AdminTiles({ editable, loaded, total }: Omit<StatsRailProps, 'isAdmin'>) {
  const connections = useManagedDatabases()
  const system = connections.data?.length
  return (
    <>
      <StatTile label="Di sistem" note="terdaftar" tone="peach" value={system} />
      <StatTile
        label="Bisa Anda akses"
        note={system === undefined ? 'database' : `dari ${system}`}
        share={system ? total / system : undefined}
        tone="lilac"
        value={loaded ? total : undefined}
      />
      <StatTile
        label="Bisa diubah"
        note={`dari ${total}`}
        share={total > 0 ? editable / total : 0}
        tone="mint"
        value={loaded ? editable : undefined}
      />
    </>
  )
}

const RING_RADIUS = 34
const RING_LENGTH = 2 * Math.PI * RING_RADIUS

interface StatTileProps {
  label: string
  note: string
  share?: number
  tone: 'peach' | 'lilac' | 'mint'
  value: number | undefined
}

// Ubin persegi: label di atas, angka (di dalam cincin bila ada pembanding) di tengah, catatan di bawah.
function StatTile({ label, note, share, tone, value }: StatTileProps) {
  const shown = value ?? '-'
  return (
    <div className={`stat stat--${tone}`}>
      <p className="stat__label">{label}</p>
      {share === undefined ? (
        <p className="stat__value">{shown}</p>
      ) : (
        <div className="stat__gauge">
          <svg aria-hidden="true" className="stat__ring" viewBox="0 0 80 80">
            <circle className="stat__ring-track" cx="40" cy="40" r={RING_RADIUS} />
            <circle
              className="stat__ring-bar"
              cx="40"
              cy="40"
              r={RING_RADIUS}
              strokeDasharray={RING_LENGTH}
              strokeDashoffset={RING_LENGTH * (1 - Math.min(share, 1))}
            />
          </svg>
          <p className="stat__value">{shown}</p>
        </div>
      )}
      <p className="stat__note">{note}</p>
    </div>
  )
}

// AdminSummary: hook admin hanya dipanggil kalau komponen ini dirender (user admin).
function AdminSummary() {
  const users = useAdminUsers()
  const groups = useAdminGroups()
  const connections = useManagedDatabases()

  return (
    <>
      <div className="section-head">
        <h2 className="section-head__title" data-area="admin">
          <span aria-hidden="true" className="area-tile">
            <Icon name="shield" size={16} />
          </span>
          Administrasi platform
        </h2>
      </div>
      <ul className="admin-summary">
        <SummaryLink
          tone="mint"
          count={users.data?.length}
          hint="Akun yang bisa masuk"
          icon="user"
          label="User"
          to="/admin/users"
        />
        <SummaryLink
          tone="lilac"
          count={groups.data?.length}
          hint="Siapa boleh akses apa"
          icon="users"
          label="Group & akses"
          to="/admin/groups"
        />
        <SummaryLink
          tone="peach"
          count={connections.data?.length}
          hint="Database terdaftar"
          icon="plug"
          label="Koneksi database"
          to="/admin/databases"
        />
      </ul>
    </>
  )
}

interface SummaryLinkProps {
  count: number | undefined
  hint: string
  icon: IconName
  label: string
  to: string
  tone: 'peach' | 'lilac' | 'mint'
}

// Seluruh ubin adalah satu target klik besar (bukan angka + tombol kecil terpisah).
function SummaryLink({ count, hint, icon, label, to, tone }: SummaryLinkProps) {
  return (
    <li>
      <Link className={`admin-summary__item admin-summary__item--${tone}`} to={to}>
        <span aria-hidden="true" className="admin-summary__icon">
          <Icon name={icon} size={20} />
        </span>
        <span className="admin-summary__label">{label}</span>
        <span className="admin-summary__count">{count ?? '-'}</span>
        <span className="admin-summary__hint">{hint}</span>
        <span className="admin-summary__go">
          Kelola
          <Icon name="arrowRight" size={14} />
        </span>
      </Link>
    </li>
  )
}
