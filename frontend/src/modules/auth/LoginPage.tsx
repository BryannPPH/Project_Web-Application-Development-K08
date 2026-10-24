import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'

import { logoMark, senja } from '@/components/brand'
import { Icon } from '@/components/Icon'
import { ApiError } from '@/lib/api-client'
import { AuthStatusPage } from '@/modules/auth/AuthStatusPage'
import { LoginForest } from '@/modules/auth/LoginForest'
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
    if (error.code === 'LOGIN_RATE_LIMITED') {
      return 'Terlalu banyak percobaan. Coba lagi dalam beberapa menit.'
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
  const [showPassword, setShowPassword] = useState(false)

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
      <section aria-hidden="true" className="login-hero">
        <LoginForest />
        <div className="login-hero__brand">
          <span className="login-hero__logo">
            <img alt="" src={logoMark} />
          </span>
          DB Manager
        </div>
        <div className="login-hero__scene">
          <p className="login-hero__bubble">
            Kelola, lihat, dan migrasikan database PostgreSQL Anda dengan aman.
          </p>
          <span className="login-hero__senja">
            <span className="login-hero__glow" />
            <img alt="" className="login-hero__mascot" src={senja.waving} />
          </span>
        </div>
      </section>

      <section aria-labelledby="login-title" className="login-panel">
        <div className="login-card">
          <header className="login-card__header">
            <img alt="" className="login-card__logo" src={logoMark} />
            <h1 id="login-title">Masuk ke akun Anda</h1>
            <p>Gunakan akun yang dibuat oleh admin platform.</p>
          </header>

          {returnPath !== '/' ? (
            <p className="login-notice" role="status">
              <Icon name="info" size={16} />
              Silakan masuk untuk melanjutkan ke halaman sebelumnya.
            </p>
          ) : null}

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
            <div className="password-field">
              <input
                autoComplete="current-password"
                disabled={loginMutation.isPending}
                id="password"
                maxLength={1024}
                minLength={8}
                name="password"
                placeholder="Masukkan kata sandi"
                required
                type={showPassword ? 'text' : 'password'}
              />
              <button
                aria-label={showPassword ? 'Sembunyikan kata sandi' : 'Tampilkan kata sandi'}
                aria-pressed={showPassword}
                className="password-field__toggle"
                disabled={loginMutation.isPending}
                onClick={() => setShowPassword((value) => !value)}
                type="button"
              >
                <Icon name={showPassword ? 'eyeOff' : 'eye'} size={18} />
              </button>
            </div>

            {loginMutation.isError ? (
              <p className="form-error" role="alert">
                <Icon name="error" size={16} />
                {loginErrorMessage(loginMutation.error)}
              </p>
            ) : null}

            <button
              className="button button--primary button--large button--block login-form__submit"
              disabled={loginMutation.isPending}
              type="submit"
            >
              {loginMutation.isPending ? 'Sedang masuk…' : 'Masuk'}
            </button>
          </form>

          <p className="login-card__footer">Lupa kata sandi? Hubungi admin platform.</p>
        </div>
      </section>
    </main>
  )
}
