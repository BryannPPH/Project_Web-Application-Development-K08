import { useEffect, useState, type ReactNode } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'

import { logoMarkLight, senja } from '@/components/brand'
import { Icon, type IconName } from '@/components/Icon'
import { ApiError } from '@/lib/api-client'
import { useTheme } from '@/lib/theme'
import { useCurrentUser } from '@/modules/auth/use-current-user'
import { useLogout } from '@/modules/auth/use-logout'
import { useAccessibleDatabase } from '@/modules/databases/api'
import '@/app/workspace.css'

// FoundationPage adalah layout workspace: sidebar, top bar, dan halaman fitur.
export function FoundationPage() {
  const currentUser = useCurrentUser()
  const logout = useLogout()
  const [menuOpen, setMenuOpen] = useState(false)
  const { pathname } = useLocation()
  const user = currentUser.data

  // Drawer mobile: Escape menutup menu, seperti dialog pada umumnya.
  useEffect(() => {
    if (!menuOpen) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMenuOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [menuOpen])

  if (!user) {
    return null
  }

  const isAdmin = user.platformRole === 'platform_admin'
  const pageKey = pathname.split('/').slice(0, 4).join('/')

  return (
    <div className={menuOpen ? 'workspace workspace--menu-open' : 'workspace'}>
      {/* Bingkai plum membungkus sidebar dan panel konten putih (gaya dashboard modern). */}
      <div className="app-frame">
        <aside className="sidebar" id="sidebar">
          <Link className="sidebar__brand" onClick={() => setMenuOpen(false)} to="/">
            <img alt="" src={logoMarkLight} />
            <span>
              <span className="sidebar__brand-accent">DB</span> Manager
            </span>
          </Link>

          <nav aria-label="Navigasi utama" className="sidebar__nav" onClick={() => setMenuOpen(false)}>
            <NavLink data-area="home" end to="/">
              <NavIcon name="dashboard" />
              Dashboard
            </NavLink>
            <NavLink data-area="database" to="/databases">
              <NavIcon name="database" />
              Database
            </NavLink>
            {/* Menu admin hanya UX; backend tetap menolak non-admin dengan 403. */}
            {isAdmin ? (
              <>
                <span className="sidebar__section">Administrasi</span>
                <NavLink data-area="admin" to="/admin/users">
                  <NavIcon name="user" />
                  User
                </NavLink>
                <NavLink data-area="admin" to="/admin/groups">
                  <NavIcon name="users" />
                  Group & akses
                </NavLink>
                <NavLink data-area="admin" to="/admin/databases">
                  <NavIcon name="plug" />
                  Koneksi database
                </NavLink>
              </>
            ) : null}
          </nav>

          {/* Akun di kaki sidebar (brief S02): siapa yang login, perannya, dan Keluar. */}
          <div className="sidebar__account">
            {/* Senja mengintip dari balik kartu akun; melambai saat disorot. */}
            <img alt="" className="sidebar__peek" src={senja.waving} />
            <span aria-hidden="true" className="sidebar__avatar">
              {user.username.charAt(0).toUpperCase()}
            </span>
            <span className="sidebar__identity">
              <span className="sidebar__username" title={user.username}>
                {user.username}
              </span>
              <span className="sidebar__role">{isAdmin ? 'Admin platform' : 'Pengguna'}</span>
            </span>
            <button
              className="sidebar__logout"
              disabled={logout.isPending}
              onClick={() => logout.mutate()}
              type="button"
            >
              <Icon
                className={logout.isPending ? 'spinner' : undefined}
                name={logout.isPending ? 'loader' : 'logout'}
                size={16}
              />
              {logout.isPending ? 'Keluar...' : 'Keluar'}
            </button>
          </div>
        </aside>

        <button
          aria-hidden="true"
          className="sidebar-backdrop"
          onClick={() => setMenuOpen(false)}
          tabIndex={-1}
          type="button"
        />

        <div className="workspace-body">
          <header className="topbar">
            <button
              aria-controls="sidebar"
              aria-expanded={menuOpen}
              aria-label={menuOpen ? 'Tutup menu' : 'Buka menu'}
              className="button button--ghost button--icon topbar__menu"
              onClick={() => setMenuOpen((open) => !open)}
              type="button"
            >
              <Icon name={menuOpen ? 'close' : 'menu'} size={20} />
            </button>
            <Breadcrumb />
            <ThemeToggle />
          </header>

          <main className="workspace-main">
            {logout.isError ? (
              <p className="form-error workspace-alert" role="alert">
                <Icon name="error" size={16} />
                {logout.error instanceof ApiError
                  ? logout.error.message
                  : 'Logout gagal. Silakan coba lagi.'}
              </p>
            ) : null}
            {/* Animasi masuk diputar ulang per halaman, bukan per klik table di viewer. */}
            <div className="page-enter" key={pageKey}>
              <Outlet />
            </div>
          </main>
        </div>
      </div>
    </div>
  )
}

const adminLabels: Record<string, string> = {
  users: 'User',
  groups: 'Group & akses',
  databases: 'Koneksi database',
}

interface Crumb {
  label: ReactNode
  to?: string
}

// Breadcrumb diturunkan dari URL agar setiap halaman otomatis punya konteks.
function Breadcrumb() {
  const { pathname } = useLocation()
  const parts = pathname.split('/').filter(Boolean).map(decodeURIComponent)
  const crumbs: Crumb[] = []

  if (parts.length === 0) {
    crumbs.push({ label: 'Dashboard' })
  } else if (parts[0] === 'databases') {
    crumbs.push({ label: 'Database', to: '/databases' })
    const [, databaseId, section, value, , table] = parts
    if (databaseId) {
      crumbs.push({ label: <DatabaseCrumb id={databaseId} />, to: `/databases/${databaseId}` })
    }
    if (section === 'schemas' && value) {
      crumbs.push({ label: value, to: `/databases/${databaseId}/schemas/${encodeURIComponent(value)}` })
      if (table) {
        crumbs.push({ label: table })
      }
    }
    if (section === 'schema-diff') {
      crumbs.push({ label: 'Schema diff' })
    }
    if (section === 'migrations') {
      crumbs.push({ label: 'Migrasi', to: `/databases/${databaseId}/migrations` })
      if (value) {
        crumbs.push({ label: value === 'new' ? 'Draft baru' : 'Detail' })
      }
    }
  } else if (parts[0] === 'admin') {
    crumbs.push({ label: 'Administrasi' })
    const label = parts[1] ? adminLabels[parts[1]] : undefined
    if (label) {
      crumbs.push({ label })
    }
  } else {
    crumbs.push({ label: 'Halaman tidak ditemukan' })
  }

  return (
    <nav aria-label="Breadcrumb" className="breadcrumb">
      <ol>
        {crumbs.map((crumb, index) => {
          const last = index === crumbs.length - 1
          return (
            <li key={index}>
              {crumb.to && !last ? (
                <Link to={crumb.to}>{crumb.label}</Link>
              ) : (
                <span aria-current={last ? 'page' : undefined}>{crumb.label}</span>
              )}
              {!last ? <Icon name="chevronRight" size={14} /> : null}
            </li>
          )
        })}
      </ol>
    </nav>
  )
}

function DatabaseCrumb({ id }: { id: string }) {
  const database = useAccessibleDatabase(id)
  return <>{database.data?.name ?? '...'}</>
}

// ThemeToggle: ganti mode terang/gelap. Ikon menunjukkan mode yang akan dipilih
// (bulan = ke mode gelap), label lengkap tetap tersedia untuk pembaca layar.
function ThemeToggle() {
  const { theme, toggle } = useTheme()
  const dark = theme === 'dark'
  return (
    <button
      aria-label={dark ? 'Ganti ke mode terang' : 'Ganti ke mode gelap'}
      aria-pressed={dark}
      className="button button--ghost button--icon topbar__theme"
      onClick={toggle}
      title={dark ? 'Mode terang' : 'Mode gelap'}
      type="button"
    >
      <Icon name={dark ? 'sun' : 'moon'} size={18} />
    </button>
  )
}

// NavIcon: ikon menu di dalam ubin berwarna sesuai area (lihat workspace.css).
function NavIcon({ name }: { name: IconName }) {
  return (
    <span aria-hidden="true" className="nav-icon">
      <Icon name={name} size={17} />
    </span>
  )
}
