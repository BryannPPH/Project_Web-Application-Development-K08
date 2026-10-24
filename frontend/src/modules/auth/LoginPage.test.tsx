import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

import { clearCsrfToken, getCsrfToken } from '@/lib/csrf-token'
import { LoginPage } from '@/modules/auth/LoginPage'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    status,
  })
}

function unauthenticatedResponse() {
  return jsonResponse(
    {
      error: {
        code: 'UNAUTHENTICATED',
        message: 'authentication required',
        requestId: 'request-me',
      },
    },
    401,
  )
}

function renderLoginPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  })

  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter
        initialEntries={[{ pathname: '/login', state: { from: '/private' } }]}
      >
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/private" element={<h1>Area privat</h1>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )

  return queryClient
}

afterEach(() => {
  cleanup()
  clearCsrfToken()
  vi.unstubAllGlobals()
})

describe('LoginPage', () => {
  it('login berhasil, mencegah submit ganda, lalu kembali ke tujuan awal', async () => {
    let resolveLogin: ((response: Response) => void) | undefined
    const loginResponse = new Promise<Response>((resolve) => {
      resolveLogin = resolve
    })
    const fetchMock = vi.fn<typeof fetch>((input) => {
      const url = String(input)
      if (url.endsWith('/auth/me')) {
        return Promise.resolve(unauthenticatedResponse())
      }
      if (url.endsWith('/auth/login')) {
        return loginResponse
      }
      throw new Error(`Unexpected request: ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderLoginPage()

    fireEvent.change(await screen.findByLabelText('Nama pengguna'), {
      target: { value: 'operator' },
    })
    fireEvent.change(screen.getByLabelText('Kata sandi'), {
      target: { value: 'secure-password' },
    })
    const submit = screen.getByRole('button', { name: 'Masuk' })
    fireEvent.click(submit)

    await waitFor(() => expect(submit).toBeDisabled())
    expect(screen.getByRole('button', { name: 'Sedang masuk…' })).toBeDisabled()
    fireEvent.click(submit)
    expect(
      fetchMock.mock.calls.filter(([input]) =>
        String(input).endsWith('/auth/login'),
      ),
    ).toHaveLength(1)

    resolveLogin?.(
      jsonResponse({
        absoluteExpiresAt: '2026-10-22T12:00:00Z',
        csrfToken: 'csrf-login',
        user: {
          id: 'user-1',
          platformRole: 'user',
          username: 'operator',
        },
      }),
    )

    expect(
      await screen.findByRole('heading', { name: 'Area privat' }),
    ).toBeInTheDocument()
    expect(getCsrfToken()).toBe('csrf-login')
  })

  it('menampilkan error generik untuk credential yang salah', async () => {
    const fetchMock = vi.fn<typeof fetch>((input) => {
      const url = String(input)
      if (url.endsWith('/auth/me')) {
        return Promise.resolve(unauthenticatedResponse())
      }
      if (url.endsWith('/auth/login')) {
        return Promise.resolve(
          jsonResponse(
            {
              error: {
                code: 'INVALID_CREDENTIALS',
                message: 'invalid username or password',
                requestId: 'request-login',
              },
            },
            401,
          ),
        )
      }
      throw new Error(`Unexpected request: ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderLoginPage()

    fireEvent.change(await screen.findByLabelText('Nama pengguna'), {
      target: { value: 'operator' },
    })
    fireEvent.change(screen.getByLabelText('Kata sandi'), {
      target: { value: 'wrong-password' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Masuk' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Nama pengguna atau kata sandi salah.',
    )
    expect(screen.getByLabelText('Kata sandi')).toHaveValue('')
  })

  it('menampilkan/menyembunyikan kata sandi dan memberi tahu tujuan setelah login', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(unauthenticatedResponse())))
    renderLoginPage()

    const password = await screen.findByLabelText('Kata sandi')
    expect(password).toHaveAttribute('type', 'password')
    expect(screen.getByRole('status')).toHaveTextContent('Silakan masuk untuk melanjutkan')

    fireEvent.click(screen.getByRole('button', { name: 'Tampilkan kata sandi' }))
    expect(password).toHaveAttribute('type', 'text')

    fireEvent.click(screen.getByRole('button', { name: 'Sembunyikan kata sandi' }))
    expect(password).toHaveAttribute('type', 'password')
  })

  it('menampilkan pesan khusus saat terlalu banyak percobaan login', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>((input) =>
        Promise.resolve(
          String(input).endsWith('/auth/login')
            ? jsonResponse(
                { error: { code: 'LOGIN_RATE_LIMITED', message: 'too many login attempts', requestId: 'r' } },
                429,
              )
            : unauthenticatedResponse(),
        ),
      ),
    )
    renderLoginPage()

    fireEvent.change(await screen.findByLabelText('Nama pengguna'), { target: { value: 'operator' } })
    fireEvent.change(screen.getByLabelText('Kata sandi'), { target: { value: 'wrong-password' } })
    fireEvent.click(screen.getByRole('button', { name: 'Masuk' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Terlalu banyak percobaan. Coba lagi dalam beberapa menit.',
    )
  })

  it('menampilkan tagline sebagai balon chat Senja', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(unauthenticatedResponse())))
    renderLoginPage()

    await screen.findByLabelText('Nama pengguna')
    expect(document.querySelector('.login-hero__bubble')).toHaveTextContent(
      'Kelola, lihat, dan migrasikan database PostgreSQL Anda dengan aman.',
    )
    expect(document.querySelector('img[src*="senja-waving"]')).toBeInTheDocument()
  })
})
