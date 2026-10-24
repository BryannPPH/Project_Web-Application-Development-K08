import { Outlet, useLocation } from 'react-router-dom'

// Navigasi antar halaman admin ada di sidebar; layout ini hanya memberi judul.
const pageTitles: Record<string, string> = {
  '/admin/users': 'User',
  '/admin/groups': 'Group & akses',
  '/admin/databases': 'Koneksi database',
}

export function AdminLayout() {
  const { pathname } = useLocation()

  return (
    <section>
      <div className="page-header">
        <div className="page-header__text">
          <h1>{pageTitles[pathname.replace(/\/$/, '')] ?? 'Administrasi platform'}</h1>
          <p className="muted">Role admin tidak memberi akses database tanpa grant group.</p>
        </div>
      </div>
      <Outlet />
    </section>
  )
}
