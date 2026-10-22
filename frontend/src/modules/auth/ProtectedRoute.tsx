import { Navigate, Outlet, useLocation } from 'react-router-dom'

import { AuthStatusPage } from '@/modules/auth/AuthStatusPage'
import { useCurrentUser } from '@/modules/auth/use-current-user'

export function ProtectedRoute() {
  const currentUser = useCurrentUser()
  const location = useLocation()

  if (currentUser.isPending) {
    return (
      <AuthStatusPage
        message="Memvalidasi sesi dan menyiapkan token keamanan."
        title="Menyiapkan ruang kerja"
      />
    )
  }

  if (currentUser.isError) {
    return (
      <AuthStatusPage
        actionLabel="Coba lagi"
        message="Sesi tidak dapat divalidasi. Pastikan backend sedang berjalan."
        onAction={() => void currentUser.refetch()}
        title="Ruang kerja belum tersedia"
      />
    )
  }

  if (!currentUser.data) {
    const from = `${location.pathname}${location.search}${location.hash}`
    return <Navigate replace state={{ from }} to="/login" />
  }

  return <Outlet />
}
