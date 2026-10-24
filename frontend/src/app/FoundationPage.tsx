import { useState, type ReactNode } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'

import { RoleBadge } from '@/components/Badges'
import { logoMarkLight, senja } from '@/components/brand'
import { Icon } from '@/components/Icon'
import { ApiError } from '@/lib/api-client'
import { useCurrentUser } from '@/modules/auth/use-current-user'
import { useLogout } from '@/modules/auth/use-logout'
import { useAccessibleDatabase } from '@/modules/databases/api'
import '@/app/workspace.css'

// FoundationPage adalah layout workspace: sidebar, top bar, dan halaman fitur.
export function FoundationPage() {
  const currentUser = useCurrentUser()
  const logout = useLogout()
  const [menuOpen, setMenuOpen] = useState(false)
  const user = currentUser.data

  if (!user) {
    return null
  }

  const isAdmin = user.platformRole === 'platform_admin'

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
            <NavLink end to="/">
              <Icon name="dashboard" size={18} />
              Dashboard
            </NavLink>
            <NavLink to="/databases">
              <Icon name="database" size={18} />
              Database
            </NavLink>
            {/* Menu admin hanya UX; backend tetap menolak non-admin dengan 403. */}
            {isAdmin ? (
              <>
                <span className="sidebar__section">Administrasi</span>
                <NavLink to="/admin/users">
                  <Icon name="user" size={18} />
                  User
                </NavLink>
                <NavLink to="/admin/groups">
                  <Icon name="users" size={18} />
                  Group &amp; akses
                </NavLink>
                <NavLink to="/admin/databases">
                  <Icon name="plug" size={18} />
                  Koneksi database
                </NavLink>
              </>
            ) : null}
          </nav>

          {/* Kartu Senja di kiri bawah sekaligus tempat tombol keluar. */}
          <div className="sidebar__card">
            <img alt="" className="sidebar__card-art" src={senja.sleeping} />
            <strong>Sudah selesai?</strong>
            <p>Jangan lupa keluar agar sesi Anda tetap aman.</p>
            <button
              className="sidebar__card-button"
              disabled={logout.isPending}
              onClick={() => logout.mutate()}
              type="button"
            >
              <Icon
                className={logout.isPending ? 'spinner' : undefined}
                name={logout.isPending ? 'loader' : 'logout'}
                size={15}
              />
              {logout.isPending ? 'Keluar…' : 'Keluar'}
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
            <div className="topbar__user">
              <div className="topbar__identity">
                <span className="topbar__username">{user.username}</span>
                <RoleBadge role={user.platformRole} />
              </div>
              <span aria-hidden="true" className="topbar__avatar">
                {user.username.charAt(0).toUpperCase()}
              </span>
            </div>
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
            <Outlet />
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
  return <>{database.data?.name ?? '…'}</>
}
