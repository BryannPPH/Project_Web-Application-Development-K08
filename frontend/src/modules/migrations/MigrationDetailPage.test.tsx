import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { AccessibleDatabase } from '@/modules/databases/types'
import { clearCsrfToken, setCsrfToken } from '@/lib/csrf-token'
import type { ApprovalReview, MigrationEntry } from '@/modules/migrations/types'
import { MigrationDetailPage } from '@/modules/migrations/MigrationDetailPage'
import { errorResponse, jsonResponse, migrationEntry, mockFetch, regularUser, renderRoute, senjaImage } from '@/test/platform-test-utils'

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
  clearCsrfToken()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

// Milestone 2: review approval untuk draft. Default "approved" supaya test lama
// (yang menguji hal lain) tetap bisa menekan tombol Jalankan.
function approvalReview(overrides: Partial<ApprovalReview> = {}): ApprovalReview {
  return {
    migrationId: 'm1',
    databaseId: 'db-1',
    action: 'apply',
    revision: 1,
    status: 'approved',
    groups: [
      {
        groupId: 'g1', groupName: 'Tim Backend', accessLevel: 'view_edit',
        leader: { userId: 'user-1', username: 'operator' },
        decision: 'approved', note: null, decidedAt: '2026-11-20T09:00:00Z',
      },
    ],
    approvedCount: 1,
    requiredCount: 1,
    canDecide: false,
    myDecision: null,
    ...overrides,
  }
}

function renderDetail(entry: MigrationEntry, target: AccessibleDatabase = database, review: ApprovalReview = approvalReview()) {
  const fetchMock = mockFetch((url) => {
    if (url.pathname === '/api/v1/databases/db-1') return jsonResponse({ database: target })
    if (url.pathname === '/api/v1/databases/db-1/migrations/m1') return jsonResponse(entry)
    if (url.pathname === '/api/v1/databases/db-1/migrations/m1/approvals') return jsonResponse({ review })
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

    expect(await screen.findByText('Migration gagal, semua perubahan di-rollback')).toBeInTheDocument()
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

  it('mencatat rekonsiliasi setelah editor mengonfirmasi pemeriksaan target', async () => {
    setCsrfToken('csrf-test')
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const unresolved = migrationEntry({ status: 'needs_reconciliation', canReconcile: true })
    const reconciled = migrationEntry({
      status: 'reconciled',
      reconciledFrom: 'needs_reconciliation',
      reconciledBy: 'user-1',
      reconciledByUsername: 'operator',
      reconciledAt: '2026-10-25T03:00:00Z',
      reconciliationNote: 'items masih belum memiliki label maupun index.',
    })
    let didReconcile = false
    const fetchMock = mockFetch((url) => {
      if (url.pathname === '/api/v1/databases/db-1') return jsonResponse({ database })
      if (url.pathname === '/api/v1/databases/db-1/migrations/m1') return jsonResponse(didReconcile ? reconciled : unresolved)
      if (url.pathname.endsWith('/m1/reconcile')) {
        didReconcile = true
        return jsonResponse(reconciled)
      }
      return undefined
    })
    renderRoute(<MigrationDetailPage />, {
      route: '/databases/db-1/migrations/m1',
      path: '/databases/:databaseId/migrations/:migrationId',
      user: regularUser,
    })

    fireEvent.change(await screen.findByLabelText('Catatan pemeriksaan'), {
      target: { value: 'items masih belum memiliki label maupun index.' },
    })
    const confirmInspection = screen.getByLabelText('Saya sudah memeriksa kondisi database target.')
    fireEvent.click(confirmInspection)
    fireEvent.click(screen.getByRole('button', { name: 'Catat rekonsiliasi' }))

    expect(await screen.findByText('Kondisi target sudah diperiksa')).toBeInTheDocument()
    expect(screen.getByText(/Ini bukan berarti SQL migration berhasil/)).toBeInTheDocument()
    const request = fetchMock.mock.calls.find(([input]) => String(input).endsWith('/m1/reconcile'))
    expect(request?.[1]?.method).toBe('POST')
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      inspectedTarget: true,
      note: 'items masih belum memiliki label maupun index.',
    })
  })

  it('tidak menyediakan aksi rekonsiliasi untuk View Only', async () => {
    renderDetail(migrationEntry({ status: 'needs_reconciliation', canEdit: false, canReconcile: false }))

    expect(await screen.findByText('Target perlu direkonsiliasi')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Catat rekonsiliasi' })).not.toBeInTheDocument()
  })

  it('draft milik view-edit dapat dijalankan setelah konfirmasi yang menyebut targetnya', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const fetchMock = renderDetail(migrationEntry({ status: 'draft', version: 7 }))

    await screen.findByText('Disetujui semua leader')
    fireEvent.click(screen.getByRole('button', { name: 'Jalankan v7' }))

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
    await screen.findByText('Disetujui semua leader')
    expect(screen.queryByText('DDL tidak didukung pada target ini')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Jalankan v1' })).toBeEnabled()
  })
  // ---------- Milestone 2 - Person A: review & approval ----------

  it('mengunci tombol Jalankan dan menjelaskan alasannya saat approval belum lengkap', async () => {
    renderDetail(migrationEntry({ status: 'draft', version: 3, revision: 2 }), database, approvalReview({
      revision: 2,
      status: 'pending',
      approvedCount: 1,
      requiredCount: 2,
      groups: [
        {
          groupId: 'g1', groupName: 'Tim Backend', accessLevel: 'view_edit',
          leader: { userId: 'u-budi', username: 'budi' }, decision: 'approved', note: null, decidedAt: '2026-11-20T09:00:00Z',
        },
        {
          groupId: 'g2', groupName: 'Analis Data', accessLevel: 'view_only',
          leader: { userId: 'u-sari', username: 'sari' }, decision: null, note: null, decidedAt: null,
        },
      ],
    }))

    expect(await screen.findByText('Menunggu 1 dari 2 leader')).toBeInTheDocument()
    const run = screen.getByRole('button', { name: 'Jalankan v3' })
    expect(run).toBeDisabled()
    expect(screen.getAllByText(/Menunggu persetujuan 1 dari 2 leader sebelum dijalankan \(revisi 2\)/).length).toBeGreaterThan(0)
    expect(screen.getByText('Menunggu 1 dari 2 leader')).toBeInTheDocument()
    expect(screen.getByText('Analis Data')).toBeInTheDocument()
    expect(screen.getByText('sari').closest('li')).toHaveTextContent('belum memutuskan')
    // Bukan leader -> tidak ada tombol keputusan.
    expect(screen.queryByRole('button', { name: /Setujui/ })).not.toBeInTheDocument()
  })

  it('menandai group tanpa leader sebagai terblokir', async () => {
    renderDetail(migrationEntry({ status: 'draft' }), database, approvalReview({
      status: 'blocked',
      approvedCount: 0,
      groups: [{ groupId: 'g1', groupName: 'Tim Backend', accessLevel: 'view_edit', leader: null, decision: null, note: null, decidedAt: null }],
    }))

    expect(await screen.findByText('Terblokir: ada group tanpa leader')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Jalankan v1' })).toBeDisabled()
    expect(screen.getByText(/Admin perlu memilih leader/)).toBeInTheDocument()
  })

  it('leader menyetujui revisi yang sedang dilihat', async () => {
    setCsrfToken('csrf-test')
    const pending = approvalReview({ status: 'pending', approvedCount: 0, canDecide: true, revision: 4,
      groups: [{ groupId: 'g1', groupName: 'Tim Backend', accessLevel: 'view_edit', leader: { userId: 'user-1', username: 'operator' }, decision: null, note: null, decidedAt: null }] })
    const approved = { ...pending, status: 'approved' as const, approvedCount: 1, myDecision: 'approved' as const,
      groups: [{ ...pending.groups[0], decision: 'approved' as const, decidedAt: '2026-11-20T09:00:00Z' }] }
    let decided = false
    const fetchMock = mockFetch((url, init) => {
      if (url.pathname === '/api/v1/databases/db-1') return jsonResponse({ database })
      if (url.pathname === '/api/v1/databases/db-1/migrations/m1') return jsonResponse(migrationEntry({ status: 'draft', revision: 4 }))
      if (url.pathname === '/api/v1/databases/db-1/migrations/m1/approvals') {
        if (init?.method === 'POST') {
          decided = true
          return jsonResponse({ review: approved }, 201)
        }
        return jsonResponse({ review: decided ? approved : pending })
      }
      return undefined
    })
    renderRoute(<MigrationDetailPage />, {
      route: '/databases/db-1/migrations/m1',
      path: '/databases/:databaseId/migrations/:migrationId',
      user: regularUser,
    })

    fireEvent.click(await screen.findByRole('button', { name: 'Setujui revisi 4' }))

    expect(await screen.findByText('Disetujui semua leader')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Jalankan v1' })).toBeEnabled()
    const request = fetchMock.mock.calls.find(([input, init]) => String(input).endsWith('/m1/approvals') && init?.method === 'POST')
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({ action: 'apply', revision: 4, decision: 'approved' })
  })

  it('menolak tanpa catatan tidak mengirim request', async () => {
    const fetchMock = renderDetail(migrationEntry({ status: 'draft' }), database, approvalReview({ status: 'pending', canDecide: true, approvedCount: 0 }))

    fireEvent.click(await screen.findByRole('button', { name: 'Tolak' }))

    expect(await screen.findByText(/Tulis alasan penolakan/)).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'POST')).toBe(false)
  })

  it('menampilkan pesan ramah saat revisi sudah basi', async () => {
    setCsrfToken('csrf-test')
    mockFetch((url, init) => {
      if (url.pathname === '/api/v1/databases/db-1') return jsonResponse({ database })
      if (url.pathname === '/api/v1/databases/db-1/migrations/m1') return jsonResponse(migrationEntry({ status: 'draft' }))
      if (url.pathname === '/api/v1/databases/db-1/migrations/m1/approvals') {
        return init?.method === 'POST'
          ? errorResponse(409, 'APPROVAL_STALE_REVISION')
          : jsonResponse({ review: approvalReview({ status: 'pending', canDecide: true, approvedCount: 0 }) })
      }
      return undefined
    })
    renderRoute(<MigrationDetailPage />, {
      route: '/databases/db-1/migrations/m1',
      path: '/databases/:databaseId/migrations/:migrationId',
      user: regularUser,
    })

    fireEvent.click(await screen.findByRole('button', { name: 'Setujui revisi 1' }))

    expect(await screen.findByText(/Migration sudah diedit sejak halaman ini dibuka/)).toBeInTheDocument()
  })

  it('memperingatkan perubahan yang tidak kompatibel', async () => {
    renderDetail(migrationEntry({ status: 'draft', sql: 'ALTER TABLE customers DROP COLUMN email;' }))

    expect(await screen.findByText('Menghapus kolom')).toBeInTheDocument()
  })
})
