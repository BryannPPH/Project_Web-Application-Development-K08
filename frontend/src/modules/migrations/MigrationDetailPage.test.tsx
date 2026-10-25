import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { AccessibleDatabase } from '@/modules/databases/types'
import type { MigrationEntry } from '@/modules/migrations/types'
import { MigrationDetailPage } from '@/modules/migrations/MigrationDetailPage'
import { jsonResponse, migrationEntry, mockFetch, regularUser, renderRoute, senjaImage } from '@/test/platform-test-utils'

const database: AccessibleDatabase = {
  id: 'db-1',
  name: 'Target Lokal',
  databaseName: 'target_db',
  engine: 'postgresql',
  capabilities: { viewer: true, transactionalMigration: true, transactionalDdl: true },
  accessLevel: 'view_edit',
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

function renderDetail(entry: MigrationEntry, target: AccessibleDatabase = database) {
  const fetchMock = mockFetch((url) => {
    if (url.pathname === '/api/v1/databases/db-1') return jsonResponse({ database: target })
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
    renderDetail(migrationEntry({
      status: 'succeeded',
      createdBy: 'df814edc-05bb-4866-8ffd-79f813170483',
      createdByUsername: 'admin_m1',
      executedBy: 'df814edc-05bb-4866-8ffd-79f813170483',
      executedByUsername: 'admin_m1',
      finishedAt: '2026-10-20T09:01:00Z',
      sql: 'CREATE TABLE public.m1_demo_items (id BIGINT PRIMARY KEY);',
    }))

    expect(await screen.findByText('Migration berhasil dijalankan')).toBeInTheDocument()
    expect(senjaImage('success')).toBeInTheDocument()
    expect(screen.getByLabelText('SQL')).toHaveValue('CREATE TABLE public.m1_demo_items (id BIGINT PRIMARY KEY);')
    expect(screen.getByLabelText('SQL')).toBeDisabled()
    expect(screen.getByText('dibuat oleh admin_m1')).toBeInTheDocument()
    expect(screen.getByText('oleh admin_m1')).toBeInTheDocument()
    expect(screen.queryByText(/df814edc/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Jalankan/ })).not.toBeInTheDocument()
  })

  it('menampilkan migration terputus dengan Senja kabel lepas', async () => {
    renderDetail(migrationEntry({ status: 'interrupted' }))

    expect(await screen.findByText('Hasil execution tidak pasti')).toBeInTheDocument()
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

  it('draft DML MySQL menjelaskan transaksi DML dan aturan DDL', async () => {
    renderDetail(
      migrationEntry({ status: 'draft', sql: "INSERT INTO bonus_mysql_manual (id, code) VALUES (1, 'ok');" }),
      { ...database, engine: 'mysql', capabilities: { viewer: true, transactionalMigration: true, transactionalDdl: false } },
    )

    expect(await screen.findByText(/DML \(SELECT, INSERT, UPDATE, DELETE\) dijalankan dalam satu transaksi/)).toBeInTheDocument()
    expect(screen.queryByText('DDL tidak didukung pada target ini')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Jalankan v1' })).toBeEnabled()
  })
})
