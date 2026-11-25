import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { clearCsrfToken, setCsrfToken } from '@/lib/csrf-token'
import type { AccessibleDatabase } from '@/modules/databases/types'
import { ExecutionHistory } from '@/modules/migrations/ExecutionHistory'
import { MigrationDetailPage } from '@/modules/migrations/MigrationDetailPage'
import type { ApprovalAction, ApprovalReview, ExecutionAttempt, MigrationEntry, MigrationJob } from '@/modules/migrations/types'
import { jsonResponse, migrationEntry, mockFetch, regularUser, renderRoute } from '@/test/platform-test-utils'

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

function review(action: ApprovalAction, overrides: Partial<ApprovalReview> = {}): ApprovalReview {
  return {
    migrationId: 'm1', databaseId: 'db-1', action, revision: 1, status: 'approved',
    groups: [{
      groupId: 'g1', groupName: 'Tim Backend', accessLevel: 'view_edit',
      leader: { userId: 'u-budi', username: 'budi' }, decision: 'approved', note: null, decidedAt: '2026-11-20T09:00:00Z',
    }],
    approvedCount: 1, requiredCount: 1, canDecide: false, myDecision: null, ...overrides,
  }
}

function job(overrides: Partial<MigrationJob> = {}): MigrationJob {
  return {
    jobId: 'job-1', databaseId: 'db-1', migrationId: 'm1', revision: 1, action: 'apply', executionMode: 'single',
    actor: 'operator', status: 'queued', retryCount: 0, createdAt: '2026-11-20T09:00:00Z', ...overrides,
  }
}

const emptyHistory = { items: [], limit: 100, offset: 0, total: 0 }

function renderDetail(
  entry: MigrationEntry,
  reviews: Partial<Record<ApprovalAction, ApprovalReview>>,
  extra?: (url: URL, init?: RequestInit) => Response | undefined,
) {
  const fetchMock = mockFetch((url, init) => {
    if (url.pathname === '/api/v1/databases/db-1') return jsonResponse({ database })
    if (url.pathname === '/api/v1/databases/db-1/migrations/m1') return jsonResponse(entry)
    if (url.pathname === '/api/v1/databases/db-1/migrations/m1/approvals') {
      const action = (url.searchParams.get('action') ?? 'apply') as ApprovalAction
      return jsonResponse({ review: reviews[action] ?? review(action) })
    }
    return extra?.(url, init)
  })
  renderRoute(<MigrationDetailPage />, {
    route: '/databases/db-1/migrations/m1',
    path: '/databases/:databaseId/migrations/:migrationId',
    user: regularUser,
  })
  return fetchMock
}

describe('aksi migration per status', () => {
  it('succeeded dengan Down SQL meminta approval rollback', async () => {
    const fetchMock = renderDetail(
      migrationEntry({ status: 'succeeded', version: 4, isActive: true, downSql: 'DROP TABLE legacy_x;' }),
      { rollback: review('rollback', { status: 'pending', approvedCount: 0, requiredCount: 1 }) },
    )

    expect(await screen.findByRole('heading', { name: 'Review & approval rollback' })).toBeInTheDocument()
    expect(await screen.findByText(/sebelum di-rollback/, { selector: '.migration-run__reason' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Rollback v4' })).toBeDisabled()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/approvals?action=rollback'))).toBe(true)
  })

  it('rolled_back meminta approval forward', async () => {
    renderDetail(migrationEntry({ status: 'rolled_back', version: 4, isActive: false }), {})

    expect(await screen.findByRole('heading', { name: 'Review & approval forward' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Jalankan ulang v4' })).toBeEnabled())
  })

  it('failed memakai approval apply untuk retry', async () => {
    renderDetail(migrationEntry({ status: 'failed', version: 2 }), {})

    expect(await screen.findByRole('button', { name: 'Coba ulang v2' })).toBeInTheDocument()
  })

  it('konfirmasi rollback menyebut Down SQL, bukan teks draft', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    renderDetail(migrationEntry({ status: 'succeeded', version: 4, isActive: true, downSql: 'DROP TABLE legacy_x;' }), {})

    await screen.findByText('Disetujui semua leader')
    fireEvent.click(screen.getByRole('button', { name: 'Rollback v4' }))

    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('Rollback v4 pada Target Lokal?'))
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('Down SQL'))
    expect(confirm).not.toHaveBeenCalledWith(expect.stringContaining('Draft tidak dapat diedit'))
  })

  it('succeeded tanpa Down SQL menjelaskan kenapa tidak bisa di-rollback', async () => {
    renderDetail(migrationEntry({ status: 'succeeded', isActive: true, downSql: null, irreversibleReason: 'DROP COLUMN menghapus data' }), {})

    expect(await screen.findByText(/tidak punya Down SQL/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Rollback/ })).not.toBeInTheDocument()
  })

  it('Jalankan mengantre job (202) lalu memantau status sampai selesai', async () => {
    setCsrfToken('csrf-test')
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    let polls = 0
    const fetchMock = renderDetail(migrationEntry({ status: 'draft', version: 7 }), {}, (url, init) => {
      if (url.pathname.endsWith('/m1/execute') && init?.method === 'POST') return jsonResponse(job(), 202)
      if (url.pathname === '/api/v1/databases/db-1/migration-jobs/job-1') {
        polls += 1
        return jsonResponse(job({ status: polls < 2 ? 'queued' : 'succeeded' }))
      }
      if (url.pathname === '/api/v1/databases/db-1/migration-history') return jsonResponse(emptyHistory)
      return undefined
    })

    await screen.findByText('Disetujui semua leader')
    fireEvent.click(screen.getByRole('button', { name: 'Jalankan v7' }))

    expect(await screen.findByText(/Eksekusi: Mengantre/)).toBeInTheDocument()
    expect(await screen.findByText(/Eksekusi: Selesai/, undefined, { timeout: 4000 })).toBeInTheDocument()
    const post = fetchMock.mock.calls.find(([input, init]) => String(input).endsWith('/m1/execute') && init?.method === 'POST')
    expect(new Headers(post?.[1]?.headers).get('Idempotency-Key')).toBeTruthy()
  })

  it('job yang ditolak worker karena approval berubah dijelaskan dengan jelas', async () => {
    setCsrfToken('csrf-test')
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderDetail(migrationEntry({ status: 'draft', version: 7 }), {}, (url, init) => {
      if (url.pathname.endsWith('/m1/execute') && init?.method === 'POST') return jsonResponse(job(), 202)
      if (url.pathname === '/api/v1/databases/db-1/migration-jobs/job-1') {
        return jsonResponse(job({ status: 'failed', errorCode: 'APPROVAL_REQUIRED', errorMessage: 'required leader approval is missing or stale' }))
      }
      return jsonResponse(emptyHistory)
    })

    await screen.findByText('Disetujui semua leader')
    fireEvent.click(screen.getByRole('button', { name: 'Jalankan v7' }))

    expect(await screen.findByText(/Worker menolak menjalankan job karena approval berubah/)).toBeInTheDocument()
  })

  it('migration batch yang partial menampilkan progres dan bisa dilanjutkan lewat Coba ulang', async () => {
    renderDetail(
      migrationEntry({ status: 'partial', version: 5, executionMode: 'batch', latestExecutionId: 'e1' }),
      {},
      (url) => {
        if (url.pathname === '/api/v1/databases/db-1/migration-executions/e1/progress') {
          return jsonResponse({ executionId: 'e1', committedBatches: 3, committedRows: 1500, lastCursor: 1500, status: 'partial' })
        }
        return undefined
      },
    )

    expect(await screen.findByText('Progres batch')).toBeInTheDocument()
    expect(await screen.findByText(/Partial: berhenti di tengah/)).toBeInTheDocument()
    expect(screen.getAllByText('1.500', { selector: 'dd' })).toHaveLength(2)
    expect(screen.getByRole('button', { name: 'Coba ulang v5' })).toBeInTheDocument()
  })
})

describe('ExecutionHistory: Hide rollback', () => {
  function attempt(id: string, action: ExecutionAttempt['action'], status: string, related: string | null = null): ExecutionAttempt {
    return {
      executionId: id, migrationId: 'm1', version: 1, revision: 1, action, actorUsername: 'operator',
      requestedAt: '2026-11-20T09:00:00Z', relatedExecutionId: related, status, errorMessage: null,
    }
  }

  function renderHistory(items: ExecutionAttempt[]) {
    mockFetch((url) => {
      if (url.pathname === '/api/v1/databases/db-1/migration-history') {
        return jsonResponse({ items, limit: 100, offset: 0, total: items.length })
      }
      return undefined
    })
    renderRoute(<ExecutionHistory databaseId="db-1" migrations={[migrationEntry({ name: 'add_points' })]} />, {
      route: '/databases/db-1/migrations', path: '/databases/:databaseId/migrations', user: regularUser,
    })
  }

  it('menyembunyikan pasangan yang dibatalkan, tetapi forward dan gagal tetap tampil', async () => {
    renderHistory([
      attempt('f1', 'forward', 'succeeded', 'r1'),
      attempt('r1', 'rollback', 'succeeded', 'a1'),
      attempt('a1', 'apply', 'succeeded'),
      attempt('a0', 'apply', 'failed'),
    ])

    await waitFor(() => expect(screen.getAllByRole('row')).toHaveLength(5))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Hide rollback' }))

    expect(screen.getAllByRole('row')).toHaveLength(3)
    expect(screen.getByText('Forward')).toBeInTheDocument()
    expect(screen.getByText('Gagal')).toBeInTheDocument()
    expect(screen.queryByText('Rollback')).not.toBeInTheDocument()
    expect(screen.getByText('2 execution disembunyikan.')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('checkbox', { name: 'Hide rollback' }))
    expect(screen.getAllByRole('row')).toHaveLength(5)
  })
})
