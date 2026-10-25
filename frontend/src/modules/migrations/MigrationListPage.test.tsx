import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { MigrationListPage } from '@/modules/migrations/MigrationListPage'
import { jsonResponse, migrationEntry, mockFetch, regularUser, renderRoute } from '@/test/platform-test-utils'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderPage(canEdit: boolean) {
  mockFetch((url) =>
    url.pathname === '/api/v1/databases/db-1/migrations'
      ? jsonResponse({ items: [], limit: 50, offset: 0, total: 0, canEdit })
      : undefined,
  )
  renderRoute(<MigrationListPage />, {
    route: '/databases/db-1/migrations',
    path: '/databases/:databaseId/migrations',
    user: regularUser,
  })
}

describe('MigrationListPage', () => {
  it('mengizinkan view-only membaca history tanpa tombol membuat draft', async () => {
    renderPage(false)

    expect(await screen.findByText('Belum ada migration')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Buat draft' })).not.toBeInTheDocument()
  })

  it('menampilkan tombol membuat draft untuk view-edit', async () => {
    renderPage(true)

    expect(await screen.findByRole('link', { name: 'Buat draft' })).toHaveAttribute(
      'href',
      '/databases/db-1/migrations/new',
    )
  })

  it('memberi catatan read-only untuk view-only', async () => {
    renderPage(false)

    expect(await screen.findByText(/Anda memiliki akses View Only/)).toBeInTheDocument()
  })

  it('menyediakan tautan detail untuk migration yang sudah berhasil bagi view-only', async () => {
    mockFetch((url) =>
      url.pathname === '/api/v1/databases/db-1/migrations'
        ? jsonResponse({
            items: [migrationEntry({ status: 'succeeded', name: 'create_m1_demo_items_v1' })],
            limit: 50,
            offset: 0,
            total: 1,
            canEdit: false,
          })
        : undefined,
    )
    renderRoute(<MigrationListPage />, {
      route: '/databases/db-1/migrations',
      path: '/databases/:databaseId/migrations',
      user: regularUser,
    })

    expect(await screen.findByRole('link', { name: 'Lihat detail' })).toHaveAttribute(
      'href',
      '/databases/db-1/migrations/m1',
    )
    expect(screen.getByRole('link', { name: 'create_m1_demo_items_v1' })).toHaveAttribute(
      'href',
      '/databases/db-1/migrations/m1',
    )
  })

  it('memperingatkan bila ada migration terputus yang memblokir execution', async () => {
    mockFetch((url) =>
      url.pathname === '/api/v1/databases/db-1/migrations'
        ? jsonResponse({
            items: [migrationEntry({ status: 'interrupted' })],
            limit: 50,
            offset: 0,
            total: 1,
            canEdit: true,
          })
        : undefined,
    )
    renderRoute(<MigrationListPage />, {
      route: '/databases/db-1/migrations',
      path: '/databases/:databaseId/migrations',
      user: regularUser,
    })

    expect(await screen.findByText('Execution diblokir')).toBeInTheDocument()
    expect(screen.getByText('Terputus')).toBeInTheDocument()
  })
})
