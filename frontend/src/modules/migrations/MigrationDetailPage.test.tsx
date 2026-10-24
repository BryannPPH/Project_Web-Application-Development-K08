import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { MigrationEntry } from '@/modules/migrations/types'
import { MigrationDetailPage } from '@/modules/migrations/MigrationDetailPage'
import { jsonResponse, migrationEntry, mockFetch, regularUser, renderRoute, senjaImage } from '@/test/platform-test-utils'

const database = { id: 'db-1', name: 'Target Lokal', databaseName: 'target_db', accessLevel: 'view_edit' }

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

function renderDetail(entry: MigrationEntry) {
  const fetchMock = mockFetch((url) => {
    if (url.pathname === '/api/v1/databases/db-1') return jsonResponse({ database })
    if (url.pathname === '/api/v1/databases/db-1/migrations/m1') return jsonResponse(entry)
    return undefined
  })
  renderRoute(<MigrationDetailPage />, {
    route: '/databases/db-1/migrations/m1',
    path: '/databases/:databaseId/migrations/:migrationId',
    user: regularUser,
  })
  return fetchMock
}

describe('MigrationDetailPage', () => {
  it('menampilkan hasil gagal dengan Senja memutar jam dan pesan error', async () => {
    renderDetail(migrationEntry({ status: 'failed', errorMessage: 'column "legacy" does not exist' }))

    expect(await screen.findByText('Migration gagal — semua perubahan di-rollback')).toBeInTheDocument()
    expect(screen.getByText('column "legacy" does not exist')).toBeInTheDocument()
    expect(senjaImage('rewind')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Jalankan/ })).not.toBeInTheDocument()
    expect(screen.getByLabelText('Nama migration')).toBeDisabled()
  })

  it('menampilkan hasil sukses dengan Senja melompat', async () => {
    renderDetail(migrationEntry({ status: 'succeeded', executedBy: 'admin', finishedAt: '2026-10-20T09:01:00Z' }))

    expect(await screen.findByText('Migration berhasil dijalankan')).toBeInTheDocument()
    expect(senjaImage('success')).toBeInTheDocument()
  })

  it('menampilkan migration terputus dengan Senja kabel lepas', async () => {
    renderDetail(migrationEntry({ status: 'interrupted' }))

    expect(await screen.findByText('Execution terputus')).toBeInTheDocument()
    expect(senjaImage('unplugged')).toBeInTheDocument()
  })

  it('draft milik view-edit dapat dijalankan setelah konfirmasi yang menyebut targetnya', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const fetchMock = renderDetail(migrationEntry({ status: 'draft', version: 7 }))

    fireEvent.click(await screen.findByRole('button', { name: 'Jalankan v7' }))

    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('Jalankan v7'))
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('satu transaksi'))
    // Konfirmasi dibatalkan: tidak ada request execute.
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/execute'))).toBe(false)
  })

  it('draft untuk view-only tidak dapat diedit atau dijalankan', async () => {
    renderDetail(migrationEntry({ status: 'draft', canEdit: false }))

    expect(await screen.findByLabelText('Nama migration')).toBeDisabled()
    expect(screen.queryByRole('button', { name: /Jalankan/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Simpan perubahan' })).not.toBeInTheDocument()
  })
})
