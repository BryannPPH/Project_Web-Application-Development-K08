import type { FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'

import { ApiError } from '@/lib/api-client'
import { AuthStatusPage } from '@/modules/auth/AuthStatusPage'
import { useCurrentUser } from '@/modules/auth/use-current-user'
import { useLogin } from '@/modules/auth/use-login'

interface LoginLocationState {
  from?: string
}

function safeReturnPath(state: unknown) {
  if (!state || typeof state !== 'object' || !('from' in state)) {
    return '/'
  }

  const from = (state as LoginLocationState).from
  return typeof from === 'string' && from.startsWith('/') && !from.startsWith('//')
    ? from
    : '/'
}

function loginErrorMessage(error: unknown) {
  if (error instanceof ApiError) {
    if (error.code === 'INVALID_CREDENTIALS') {
      return 'Nama pengguna atau kata sandi salah.'
    }
    return error.message
  }

  return 'Terjadi kesalahan. Silakan coba lagi.'
}

export function LoginPage() {
  const currentUser = useCurrentUser()
  const loginMutation = useLogin()
  const location = useLocation()
  const navigate = useNavigate()
  const returnPath = safeReturnPath(location.state)

  if (currentUser.isPending) {
    return (
      <AuthStatusPage
        message="Memeriksa sesi aktif Anda."
        title="Menyiapkan aplikasi"
      />
    )
  }

  if (currentUser.isError) {
    return (
      <AuthStatusPage
        actionLabel="Coba lagi"
        message="Status sesi tidak dapat diperiksa. Pastikan backend sedang berjalan."
        onAction={() => void currentUser.refetch()}
        title="Tidak dapat terhubung"
      />
    )
  }

  if (currentUser.data) {
    return <Navigate replace to={returnPath} />
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const formData = new FormData(form)
    const username = formData.get('username')
    const password = formData.get('password')

    if (typeof username !== 'string' || typeof password !== 'string') {
      return
    }

    try {
      await loginMutation.mutateAsync({ username: username.trim(), password })
      void navigate(returnPath, { replace: true })
    } catch {
      const passwordInput = form.elements.namedItem('password')
      if (passwordInput instanceof HTMLInputElement) {
        passwordInput.value = ''
        passwordInput.focus()
      }
    }
  }

  return (
    <main className="login-page">
      <section className="login-intro" aria-labelledby="login-title">
        <div className="brand-mark" aria-hidden="true">
          DB
        </div>
        <p className="login-intro__eyebrow">Database Management Platform</p>
        <h1 id="login-title">Kelola basis data dengan akses yang terkendali.</h1>
        <p className="login-intro__description">
          Satu ruang kerja untuk melihat data, mengatur akses, dan menjalankan
          migrasi secara aman.
        </p>
        <ul className="login-intro__features" aria-label="Keunggulan platform">
          <li>Kontrol akses berbasis grup</li>
          <li>Sesi aman dengan perlindungan CSRF</li>
          <li>Riwayat migrasi yang dapat ditelusuri</li>
        </ul>
      </section>

      <section className="login-panel" aria-labelledby="login-form-title">
        <div className="login-panel__header">
          <p className="login-panel__eyebrow">Selamat datang</p>
          <h2 id="login-form-title">Masuk ke akun Anda</h2>
          <p>Gunakan akun platform yang telah diberikan administrator.</p>
        </div>

        <form className="login-form" onSubmit={handleSubmit}>
          <label htmlFor="username">Nama pengguna</label>
          <input
            autoComplete="username"
            autoFocus
            disabled={loginMutation.isPending}
            id="username"
            maxLength={64}
            minLength={3}
            name="username"
            placeholder="Masukkan nama pengguna"
            required
            type="text"
          />

          <label htmlFor="password">Kata sandi</label>
          <input
            autoComplete="current-password"
            disabled={loginMutation.isPending}
            id="password"
            maxLength={1024}
            minLength={8}
            name="password"
            placeholder="Masukkan kata sandi"
            required
            type="password"
          />

          {loginMutation.isError ? (
            <p className="form-error" role="alert">
              {loginErrorMessage(loginMutation.error)}
            </p>
          ) : null}

          <button
            className="button button--primary login-form__submit"
            disabled={loginMutation.isPending}
            type="submit"
          >
            {loginMutation.isPending ? 'Sedang masuk…' : 'Masuk'}
          </button>
        </form>
      </section>
    </main>
  )
}
