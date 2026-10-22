import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

import { clearCsrfToken, getCsrfToken } from '@/lib/csrf-token'
import { ProtectedRoute } from '@/modules/auth/ProtectedRoute'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    status,
  })
}

function renderProtectedRoute() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/private?tab=data']}>
        <Routes>
          <Route element={<ProtectedRoute />}>
            <Route path="/private" element={<h1>Konten privat</h1>} />
          </Route>
          <Route path="/login" element={<h1>Halaman login</h1>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

afterEach(() => {
  clearCsrfToken()
  vi.unstubAllGlobals()
})

describe('ProtectedRoute', () => {
  it('mengalihkan pengguna tanpa sesi ke halaman login', async () => {
    const fetchMock = vi.fn<typeof fetch>()
    fetchMock.mockResolvedValue(
      jsonResponse(
        {
          error: {
            code: 'UNAUTHENTICATED',
            message: 'authentication required',
            requestId: 'request-1',
          },
        },
        401,
      ),
    )
    vi.stubGlobal('fetch', fetchMock)
    renderProtectedRoute()

    expect(
      await screen.findByRole('heading', { name: 'Halaman login' }),
    ).toBeInTheDocument()
  })

  it('memuat CSRF sebelum menampilkan route terproteksi', async () => {
    const fetchMock = vi.fn<typeof fetch>((input) => {
      const url = String(input)
      if (url.endsWith('/auth/me')) {
        return Promise.resolve(
          jsonResponse({
            user: {
              id: 'user-1',
              platformRole: 'user',
              username: 'operator',
            },
          }),
        )
      }
      if (url.endsWith('/auth/csrf')) {
        return Promise.resolve(jsonResponse({ csrfToken: 'csrf-reload' }))
      }
      throw new Error(`Unexpected request: ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderProtectedRoute()

    expect(
      await screen.findByRole('heading', { name: 'Konten privat' }),
    ).toBeInTheDocument()
    expect(getCsrfToken()).toBe('csrf-reload')
  })
})
