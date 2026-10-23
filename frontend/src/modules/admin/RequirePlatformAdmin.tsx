import { Link, Outlet } from 'react-router-dom'

import { StatusMessage } from '@/components/StatusMessage'
import { useCurrentUser } from '@/modules/auth/use-current-user'

// Guard UX untuk halaman admin. Ini BUKAN kontrol keamanan: backend tetap
// menolak user non-admin dengan 403 pada setiap endpoint /admin.
export function RequirePlatformAdmin() {
  const currentUser = useCurrentUser()

  if (currentUser.data?.platformRole !== 'platform_admin') {
    return (
      <StatusMessage title="Akses ditolak" tone="error">
        Halaman ini hanya untuk administrator platform. <Link to="/">Kembali ke dashboard</Link>
      </StatusMessage>
    )
  }

  return <Outlet />
}
