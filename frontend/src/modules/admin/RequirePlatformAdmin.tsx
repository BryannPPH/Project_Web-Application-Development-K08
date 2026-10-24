import { Link, Outlet } from 'react-router-dom'

import { senja } from '@/components/brand'
import { Icon } from '@/components/Icon'
import { useCurrentUser } from '@/modules/auth/use-current-user'

// Guard UX untuk halaman admin. Ini BUKAN kontrol keamanan: backend tetap
// menolak user non-admin dengan 403 pada setiap endpoint /admin.
export function RequirePlatformAdmin() {
  const currentUser = useCurrentUser()

  if (currentUser.data?.platformRole !== 'platform_admin') {
    return (
      <div className="error-page" role="alert">
        <img alt="" className="error-page__art" src={senja.locked} />
        <h1>Akses ditolak</h1>
        <p>Halaman ini hanya untuk admin platform.</p>
        <Link className="button button--primary" to="/">
          <Icon name="dashboard" size={16} />
          Kembali ke Dashboard
        </Link>
      </div>
    )
  }

  return <Outlet />
}
