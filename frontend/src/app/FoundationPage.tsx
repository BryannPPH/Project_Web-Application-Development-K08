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
        <a className="workspace-brand" href="/" aria-label="DB Manager">
          <span className="brand-mark brand-mark--small" aria-hidden="true">
            DB
          </span>
          <span>
            <strong>DB Manager</strong>
            <small>Management Platform</small>
          </span>
        </a>

        <div className="workspace-user">
          <span className="workspace-user__avatar" aria-hidden="true">
            {user.username.slice(0, 1).toUpperCase()}
          </span>
          <span className="workspace-user__identity">
            <strong>{user.username}</strong>
            <small>{roleLabel(user.platformRole)}</small>
          </span>
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
        <section className="workspace-hero" aria-labelledby="workspace-title">
          <p className="workspace-hero__eyebrow">Ruang kerja</p>
          <h1 id="workspace-title">Selamat datang, {user.username}.</h1>
          <p>
            Sesi Anda telah terautentikasi. Modul basis data, viewer, dan migrasi
            akan terhubung melalui fondasi keamanan yang sama.
          </p>
        </section>

        {logout.isError ? (
          <p className="workspace-alert" role="alert">
            {logout.error instanceof ApiError
              ? logout.error.message
              : 'Logout gagal. Silakan coba lagi.'}
          </p>
        ) : null}

        <section className="workspace-grid" aria-label="Status modul">
          <article className="workspace-card">
            <span className="workspace-card__index">01</span>
            <h2>Sesi aman</h2>
            <p>Cookie HttpOnly dan CSRF aktif untuk melindungi setiap perubahan.</p>
            <span className="status-pill status-pill--ready">Aktif</span>
          </article>
          <article className="workspace-card">
            <span className="workspace-card__index">02</span>
            <h2>Akses basis data</h2>
            <p>Daftar basis data akan mengikuti izin efektif dari grup pengguna.</p>
            <span className="status-pill">Menunggu integrasi</span>
          </article>
          <article className="workspace-card">
            <span className="workspace-card__index">03</span>
            <h2>Riwayat migrasi</h2>
            <p>Eksekusi dan hasil migrasi akan ditampilkan secara terstruktur.</p>
            <span className="status-pill">Menunggu integrasi</span>
          </article>
        </section>
      </main>
    </div>
  )
}

