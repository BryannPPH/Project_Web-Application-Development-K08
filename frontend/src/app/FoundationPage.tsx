import { NavLink, Outlet } from 'react-router-dom'

import { ApiError } from '@/lib/api-client'
import { useCurrentUser } from '@/modules/auth/use-current-user'
import { useLogout } from '@/modules/auth/use-logout'
import '@/app/workspace.css'

// FoundationPage adalah layout workspace: header, navigasi, dan halaman fitur.
export function FoundationPage() {
  const currentUser = useCurrentUser()
  const logout = useLogout()
  const user = currentUser.data

  if (!user) {
    return null
  }

  return (
    <div className="workspace">
      <header className="workspace-header">
        <a className="workspace-brand" href="/">DB Manager</a>

        <nav aria-label="Navigasi utama" className="workspace-nav">
          <NavLink end to="/">
            Dashboard
          </NavLink>
          <NavLink to="/databases">Database</NavLink>
          {/* Menu admin hanya UX; backend tetap menolak non-admin dengan 403. */}
          {user.platformRole === 'platform_admin' ? <NavLink to="/admin">Admin</NavLink> : null}
        </nav>

        <div className="workspace-user">
          <span>{user.username}</span>
          <button
            className="button button--secondary"
            disabled={logout.isPending}
            onClick={() => logout.mutate()}
            type="button"
          >
            {logout.isPending ? 'Keluar…' : 'Keluar'}
          </button>
        </div>
      </header>

      <main className="workspace-main">
        {logout.isError ? (
          <p className="workspace-alert" role="alert">
            {logout.error instanceof ApiError
              ? logout.error.message
              : 'Logout gagal. Silakan coba lagi.'}
          </p>
        ) : null}
        <Outlet />
      </main>
    </div>
  )
}
