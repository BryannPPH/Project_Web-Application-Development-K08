/*
 * FILE INI UNTUK APA (DashboardPage.tsx)
 * GUIDEBOOK: §2.8
 * Halaman awal setelah login:
 *   - Kepala halaman: tanggal, sapaan sesuai waktu (pagi/siang/sore/malam),
 *     ringkasan akses satu kalimat, dan Senja (rig animasi) berkostum sesuai waktu:
 *     pagi menggeliat dengan mug kopi, siang melambai bertopi jerami + es kopi,
 *     sore santai membawa lentera, malam terkantuk-kantuk memeluk bantal.
 *     Kepala halaman disorot -> Senja melambai (kecuali malam: Senja sedang tidur).
 *   - Kartu database yang bisa dibuka (tugas paling sering) tepat di bawahnya.
 *   - Untuk platform_admin: tiga ubin cepat ke User, Group & akses, Koneksi.
 */
import { useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import type { SenjaClipName } from '@/assets/brand/senja-rig/web/senja-data'
import { describeError } from '@/components/error-message'
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
      {/* Kepala halaman memakai pola yang sama dengan halaman lain (tanpa kotak, di atas
          latar halaman). Waktu hari cukup dibawa Senja lewat kostumnya; Senja berdiri di
          garis pemisah kepala halaman. */}
      <header className={`hero hero--${part}`} ref={heroRef}>
        <div className="hero__text">
          <p className="hero__date">{todayFormat.format(now)}</p>
          <h1 className="hero__greeting">
            Selamat {part}, <span className="hero__name">{user.username}</span>
          </h1>
          <p className="hero__summary">
            <AccessSummary editable={editable} loaded={databases.isSuccess} total={list.length} />
          </p>
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
    </section>
  )
}

// Ringkasan akses sebagai satu kalimat; angka ditebalkan (tabular) agar tetap mudah dipindai.
function AccessSummary({ loaded, total, editable }: { loaded: boolean; total: number; editable: number }) {
  if (!loaded) return <>Memuat akses Anda...</>
  if (total === 0) return <>Anda belum punya akses ke database mana pun.</>
  const viewOnly = total - editable
  const parts = [
    editable > 0 ? <span key="edit"><strong>{editable}</strong> bisa diubah</span> : null,
    viewOnly > 0 ? <span key="view"><strong>{viewOnly}</strong> hanya lihat</span> : null,
  ].filter(Boolean)
  return (
    <>
      Anda punya akses ke <strong>{total}</strong> database
      {parts.length > 0 ? ': ' : '.'}
      {parts.map((part, index) => (
        <span key={index}>
          {index > 0 ? ', ' : null}
          {part}
        </span>
      ))}
      {parts.length > 0 ? '.' : null}
    </>
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
          count={users.data?.length}
          hint="Akun yang bisa masuk"
          icon="user"
          label="User"
          to="/admin/users"
        />
        <SummaryLink
          count={groups.data?.length}
          hint="Siapa boleh akses apa"
          icon="users"
          label="Group & akses"
          to="/admin/groups"
        />
        <SummaryLink
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
}

// Seluruh ubin adalah satu target klik besar (bukan angka + tombol kecil terpisah).
function SummaryLink({ count, hint, icon, label, to }: SummaryLinkProps) {
  return (
    <li>
      <Link className="admin-summary__item" to={to}>
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
