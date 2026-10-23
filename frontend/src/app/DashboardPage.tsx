import { Link } from 'react-router-dom'

import { useCurrentUser } from '@/modules/auth/use-current-user'

export function DashboardPage() {
  const currentUser = useCurrentUser()
  const user = currentUser.data

  if (!user) {
    return null
  }

  return (
    <section>
      <h1>Dashboard</h1>
      <p className="workspace-role">
        {user.platformRole === 'platform_admin' ? 'Administrator platform' : 'Pengguna'}
      </p>
      <div className="workspace-links">
        <Link className="button button--primary" to="/databases">
          Buka database
        </Link>
        {user.platformRole === 'platform_admin' ? (
          <Link className="button button--secondary" to="/admin">
            Administrasi platform
          </Link>
        ) : null}
      </div>
    </section>
  )
}
