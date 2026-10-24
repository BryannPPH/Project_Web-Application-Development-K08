import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { DatabaseListPage } from '@/modules/databases/DatabaseListPage'
import {
  adminUser,
  errorResponse,
  jsonResponse,
  mockFetch,
  regularUser,
  renderRoute,
  senjaImage,
} from '@/test/platform-test-utils'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderPage() {
  renderRoute(<DatabaseListPage />, { route: '/databases', path: '/databases', user: regularUser })
}

describe('DatabaseListPage', () => {
  it('menampilkan hanya database yang dapat diakses beserta levelnya', async () => {
    mockFetch((url) =>
      url.pathname === '/api/v1/databases'
        ? jsonResponse({
            databases: [
              { id: 'db-1', name: 'Warehouse', databaseName: 'warehouse', engine: 'postgresql', capabilities: { viewer: true, transactionalMigration: true, transactionalDdl: true }, accessLevel: 'view_only' },
              { id: 'db-2', name: 'Sales', databaseName: 'sales', engine: 'mysql', capabilities: { viewer: true, transactionalMigration: true, transactionalDdl: false }, accessLevel: 'view_edit' },
            ],
          })
        : undefined,
    )
    renderPage()

    expect(await screen.findByText('Warehouse')).toBeInTheDocument()
    expect(screen.getByText('View Only')).toBeInTheDocument()
    expect(screen.getByText('View + Edit')).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: 'Buka viewer' })[0]).toHaveAttribute('href', '/databases/db-1')
    expect(screen.getAllByRole('link', { name: 'Riwayat migration' })[0]).toHaveAttribute(
      'href',
      '/databases/db-1/migrations',
    )
  })

  it('menampilkan empty state bila tidak ada grant', async () => {
    mockFetch((url) => (url.pathname === '/api/v1/databases' ? jsonResponse({ databases: [] }) : undefined))
    renderPage()

    expect(await screen.findByText('Belum ada database')).toBeInTheDocument()
  })

  it('menampilkan error state', async () => {
    mockFetch((url) =>
      url.pathname === '/api/v1/databases' ? errorResponse(500, 'INTERNAL_ERROR', 'internal server error') : undefined,
    )
    renderPage()

    // Hook me-retry error 5xx satu kali (jeda ~1 detik) sebelum menampilkan error.
    expect(await screen.findByRole('alert', {}, { timeout: 4000 })).toHaveTextContent('Daftar database gagal dimuat')
  })

  it('menyaring database lewat pencarian dan filter akses', async () => {
    mockFetch((url) =>
      url.pathname === '/api/v1/databases'
        ? jsonResponse({
            databases: [
              { id: 'db-1', name: 'Warehouse', databaseName: 'warehouse', accessLevel: 'view_only' },
              { id: 'db-2', name: 'Sales', databaseName: 'sales_prod', accessLevel: 'view_edit' },
            ],
          })
        : undefined,
    )
    renderPage()

    fireEvent.change(await screen.findByLabelText('Cari database'), { target: { value: 'SALES_P' } })
    expect(screen.getByText('Sales')).toBeInTheDocument()
    expect(screen.queryByText('Warehouse')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Hanya lihat' }))
    expect(screen.getByText('Tidak ada database yang cocok dengan filter.')).toBeInTheDocument()
    expect(senjaImage('searching')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Cari database'), { target: { value: '' } })
    expect(screen.getByText('Warehouse')).toBeInTheDocument()
    expect(screen.queryByText('Sales')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Hanya lihat' })).toHaveAttribute('aria-pressed', 'true')

    fireEvent.change(screen.getByLabelText('Cari database'), { target: { value: 'tidak-ada' } })
    fireEvent.click(screen.getByRole('button', { name: 'Hapus filter' }))
    expect(screen.getByText('Warehouse')).toBeInTheDocument()
    expect(screen.getByText('Sales')).toBeInTheDocument()
    expect(screen.getByLabelText('Cari database')).toHaveValue('')
  })

  it('menampilkan Senja mencari saat kosong, tanpa petunjuk admin untuk user biasa', async () => {
    mockFetch((url) => (url.pathname === '/api/v1/databases' ? jsonResponse({ databases: [] }) : undefined))
    renderPage()

    expect(await screen.findByText('Belum ada database')).toBeInTheDocument()
    expect(senjaImage('searching')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /Group &/ })).not.toBeInTheDocument()
  })

  it('memberi admin tanpa grant petunjuk ke halaman group', async () => {
    mockFetch((url) => (url.pathname === '/api/v1/databases' ? jsonResponse({ databases: [] }) : undefined))
    renderRoute(<DatabaseListPage />, { route: '/databases', path: '/databases', user: adminUser })

    expect(await screen.findByRole('link', { name: /Group &/ })).toHaveAttribute('href', '/admin/groups')
  })
})
