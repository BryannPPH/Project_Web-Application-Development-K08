import { NavLink, Outlet } from 'react-router-dom'

import '@/components/platform.css'

export function AdminLayout() {
  return (
    <section>
      <div className="page-header">
        <h1>Administrasi platform</h1>
        <span className="muted">Role admin tidak memberi akses database tanpa grant group.</span>
      </div>
      <nav aria-label="Menu admin" className="tab-list">
        <NavLink to="/admin/users">User</NavLink>
        <NavLink to="/admin/groups">Group &amp; akses</NavLink>
        <NavLink to="/admin/databases">Koneksi database</NavLink>
      </nav>
      <Outlet />
    </section>
  )
}
