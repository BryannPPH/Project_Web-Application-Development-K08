import { cleanup, screen } from '@testing-library/react'
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

    expect(screen.getByRole('heading', { name: 'Halo, operator' })).toBeInTheDocument()
    expect(await screen.findByText(/Minta admin menambahkan Anda ke group/)).toBeInTheDocument()
    expect(senjaImage('empty-box')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Administrasi platform' })).not.toBeInTheDocument()
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

    expect(await screen.findByText('Database tersedia')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Tambah user' })).toHaveAttribute('href', '/admin/users')
    expect(screen.getByRole('link', { name: 'Administrasi platform' })).toHaveAttribute('href', '/admin')
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
