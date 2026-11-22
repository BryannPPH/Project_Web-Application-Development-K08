import { cleanup, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { DashboardPage } from '@/app/DashboardPage'
import { NotFoundPage } from '@/app/NotFoundPage'
import { adminUser, jsonResponse, mockFetch, regularUser, renderRoute, senjaImage } from '@/test/platform-test-utils'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('DashboardPage', () => {
  it('menyapa user dan menampilkan Senja bila belum punya akses database', async () => {
    mockFetch((url) => (url.pathname === '/api/v1/databases' ? jsonResponse({ databases: [] }) : undefined))
    renderRoute(<DashboardPage />, { route: '/', path: '/', user: regularUser })

    expect(screen.getByRole('heading', { name: /^Selamat (pagi|siang|sore|malam), operator$/ })).toBeInTheDocument()
    expect(await screen.findByText(/Minta admin menambahkan Anda ke group/)).toBeInTheDocument()
    expect(senjaImage('empty-box')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Administrasi platform' })).not.toBeInTheDocument()
  })

  it.each([
    [7, 'pagi', 'idle'],
    [12, 'siang', 'idle'],
    [17, 'sore', 'idle'],
    [22, 'malam', 'sleepy'],
  ])('jam %i: banner %s memakai Senja berkostum sesuai waktunya', (hour, part, loop) => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 10, 17, hour, 0))
    try {
      mockFetch((url) => (url.pathname === '/api/v1/databases' ? jsonResponse({ databases: [] }) : undefined))
      renderRoute(<DashboardPage />, { route: '/', path: '/', user: regularUser })

      expect(screen.getByRole('heading', { name: `Selamat ${part}, operator` })).toBeInTheDocument()
      const mascot = document.querySelector('.hero__art')
      expect(mascot).toHaveAttribute('data-senja-loop', loop)
      expect(mascot).toHaveAttribute('data-outfit', part)
      expect(document.querySelector('.hero')).toHaveClass(`hero--${part}`)
      expect(mascot?.querySelector('svg')).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('menampilkan ringkasan platform untuk admin', async () => {
    mockFetch((url) => {
      switch (url.pathname) {
        case '/api/v1/databases':
          return jsonResponse({
            databases: [
              { id: 'db-1', name: 'A', databaseName: 'a', accessLevel: 'view_edit' },
              { id: 'db-2', name: 'B', databaseName: 'b', accessLevel: 'view_only' },
            ],
          })
        case '/api/v1/admin/users':
          return jsonResponse({ users: [{}, {}, {}] })
        case '/api/v1/admin/groups':
          return jsonResponse({ groups: [{}] })
        case '/api/v1/admin/databases':
          return jsonResponse({ databases: [{}, {}] })
        default:
          return undefined
      }
    })
    renderRoute(<DashboardPage />, { route: '/', path: '/', user: adminUser })

    // Database yang bisa diakses tampil paling atas, lengkap dengan tautan viewer.
    expect(await screen.findByRole('link', { name: 'A' })).toHaveAttribute('href', '/databases/db-1')
    // Angka akses ada di rel widget (bukan kalimat ringkasan di banner).
    const rail = screen.getByRole('complementary', { name: 'Ringkasan database' })
    await waitFor(() => expect(rail).toHaveTextContent(/Di sistem\s*2\s*terdaftar/))
    expect(rail).toHaveTextContent(/Bisa Anda akses\s*2\s*dari 2/)
    expect(rail).toHaveTextContent(/Bisa diubah\s*1\s*dari 2/)

    // Ringkasan platform: tiap angka adalah tautan ke halaman admin-nya.
    expect(screen.getByRole('heading', { name: 'Administrasi platform' })).toBeInTheDocument()
    const tile = async (hint: string) => (await screen.findByText(hint)).closest('a')
    await waitFor(async () => expect(await tile('Akun yang bisa masuk')).toHaveTextContent(/User\s*3/))
    expect(await tile('Akun yang bisa masuk')).toHaveAttribute('href', '/admin/users')
    await waitFor(async () => expect(await tile('Siapa boleh akses apa')).toHaveTextContent(/Group & akses\s*1/))
    expect(await tile('Siapa boleh akses apa')).toHaveAttribute('href', '/admin/groups')
    await waitFor(async () => expect(await tile('Database terdaftar')).toHaveTextContent(/Koneksi database\s*2/))
    expect(await tile('Database terdaftar')).toHaveAttribute('href', '/admin/databases')
  })
})

describe('NotFoundPage', () => {
  it('menampilkan Senja membaca peta dan jalan kembali ke dashboard', () => {
    renderRoute(<NotFoundPage />, { route: '/tidak-ada', path: '*' })

    expect(screen.getByRole('heading', { name: 'Halaman tidak ditemukan' })).toBeInTheDocument()
    expect(senjaImage('lost-map')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Kembali ke Dashboard' })).toHaveAttribute('href', '/')
  })
})
