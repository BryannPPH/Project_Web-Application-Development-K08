import { describe, expect, it } from 'vitest'

import { hideRolledBack } from '@/modules/migrations/history'
import type { ExecutionAttempt } from '@/modules/migrations/types'

function attempt(id: string, action: ExecutionAttempt['action'], status: string, related: string | null = null): ExecutionAttempt {
  return {
    executionId: id, migrationId: 'm1', version: 1, revision: 1, action, actorUsername: 'operator',
    requestedAt: '2026-11-20T09:00:00Z', relatedExecutionId: related, status, errorMessage: null,
  }
}

const ids = (items: ExecutionAttempt[]) => items.map((item) => item.executionId)

describe('hideRolledBack', () => {
  it('menyembunyikan execution yang dibatalkan beserta rollback suksesnya', () => {
    const items = [attempt('r1', 'rollback', 'succeeded', 'a1'), attempt('a1', 'apply', 'succeeded')]
    expect(hideRolledBack(items)).toEqual([])
  })

  it('forward dan attempt gagal selalu tetap tampil', () => {
    const items = [
      attempt('f1', 'forward', 'succeeded', 'r1'),
      attempt('r1', 'rollback', 'succeeded', 'a1'),
      attempt('a1', 'apply', 'succeeded'),
      attempt('a0', 'apply', 'failed'),
    ]
    expect(ids(hideRolledBack(items))).toEqual(['f1', 'a0'])
  })

  it('rollback yang gagal atau sebagian tidak menyembunyikan apa pun', () => {
    const items = [
      attempt('r2', 'rollback', 'partially_reverted', 'a1'),
      attempt('r1', 'rollback', 'failed', 'a1'),
      attempt('a1', 'apply', 'succeeded'),
    ]
    expect(ids(hideRolledBack(items))).toEqual(['r2', 'r1', 'a1'])
  })

  it('forward yang kemudian di-rollback tetap tampil bersama rollback-nya', () => {
    const items = [attempt('r2', 'rollback', 'succeeded', 'f1'), attempt('f1', 'forward', 'succeeded', 'r1')]
    expect(ids(hideRolledBack(items))).toEqual(['r2', 'f1'])
  })

  it('rollback yatim (target di luar halaman) tidak disembunyikan', () => {
    expect(ids(hideRolledBack([attempt('r1', 'rollback', 'succeeded', 'gone')]))).toEqual(['r1'])
  })
})
