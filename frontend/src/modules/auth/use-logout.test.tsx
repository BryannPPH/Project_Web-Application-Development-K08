import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

import {
  clearCsrfToken,
  getCsrfToken,
  setCsrfToken,
} from '@/lib/csrf-token'
import { useLogout } from '@/modules/auth/use-logout'

function LogoutHarness() {
  const logout = useLogout()
  return (
    <button onClick={() => logout.mutate()} type="button">
      Keluar
    </button>
  )
}

afterEach(() => {
  clearCsrfToken()
  vi.unstubAllGlobals()
})

describe('useLogout', () => {
  it('membersihkan query cache setelah logout berhasil', async () => {
    const fetchMock = vi.fn<typeof fetch>()
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ status: 'logged_out' }), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )
    vi.stubGlobal('fetch', fetchMock)
    setCsrfToken('csrf-logout')

    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })
    queryClient.setQueryData(['private-data'], { secret: true })

    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/private']}>
          <Routes>
            <Route path="/private" element={<LogoutHarness />} />
            <Route path="/login" element={<h1>Halaman login</h1>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Keluar' }))

    expect(
      await screen.findByRole('heading', { name: 'Halaman login' }),
    ).toBeInTheDocument()
    await waitFor(() =>
      expect(queryClient.getQueryData(['private-data'])).toBeUndefined(),
    )
    expect(getCsrfToken()).toBeNull()

    const [, options] = fetchMock.mock.calls[0]!
    expect(new Headers(options?.headers).get('X-CSRF-Token')).toBe(
      'csrf-logout',
    )
  })
})
