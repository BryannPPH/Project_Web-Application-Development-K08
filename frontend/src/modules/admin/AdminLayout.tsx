import { Outlet, useLocation } from 'react-router-dom'

// Navigasi antar halaman admin ada di sidebar; layout ini hanya memberi judul.
// Tiap halaman menjelaskan apa yang diaturnya, bukan satu kalimat yang sama di semua halaman.
const pages: Record<string, { title: string; description: string }> = {
  '/admin/users': {
    title: 'User',
    description:
      'Akun yang bisa masuk ke platform. Mengubah role atau menonaktifkan user akan mencabut semua sesinya.',
  },
  '/admin/groups': {
    title: 'Group & akses',
    description:
      'Akses database diberikan lewat group. Akses seorang user = level tertinggi dari semua group yang diikutinya.',
  },
  '/admin/databases': {
    title: 'Koneksi database',
    description:
      'Database yang terdaftar di platform. Password disimpan terenkripsi dan tidak pernah ditampilkan kembali.',
  },
}

export function AdminLayout() {
  const { pathname } = useLocation()
  const page = pages[pathname.replace(/\/$/, '')]

  return (
    <section>
      <div className="page-header">
        <div className="page-header__text">
          <h1>{page?.title ?? 'Administrasi platform'}</h1>
          <p>{page?.description ?? 'Role admin tidak memberi akses database tanpa grant group.'}</p>
        </div>
      </div>
      <Outlet />
    </section>
  )
}
