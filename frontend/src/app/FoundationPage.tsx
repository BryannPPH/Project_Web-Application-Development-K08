import { ApiError } from '@/lib/api-client'
import { useCurrentUser } from '@/modules/auth/use-current-user'
import { useLogout } from '@/modules/auth/use-logout'
import '@/app/workspace.css'

function roleLabel(role: 'user' | 'platform_admin') {
  return role === 'platform_admin' ? 'Administrator platform' : 'Pengguna'
}

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
        <h1>Dashboard</h1>
        <p className="workspace-role">{roleLabel(user.platformRole)}</p>

        {logout.isError ? (
          <p className="workspace-alert" role="alert">
            {logout.error instanceof ApiError
              ? logout.error.message
              : 'Logout gagal. Silakan coba lagi.'}
          </p>
        ) : null}
      </main>
    </div>
  )
}

