import { describe, expect, it } from 'vitest'

import { nextAction } from '@/modules/migrations/actions'

const reversible = { isActive: true, downSql: 'DROP TABLE x;', irreversibleReason: null }

describe('nextAction', () => {
  it.each([
    ['draft', 'apply', 'apply'],
    ['failed', 'retry', 'apply'],
    ['partial', 'retry', 'apply'],
    ['succeeded', 'rollback', 'rollback'],
    ['partially_reverted', 'rollback', 'rollback'],
    ['rolled_back', 'forward', 'forward'],
  ] as const)('%s -> %s memakai approval %s', (status, action, approval) => {
    const next = nextAction({ status, ...reversible })
    expect(next?.action).toBe(action)
    expect(next?.approval).toBe(approval)
  })

  it('tidak menawarkan rollback tanpa down SQL atau saat ditandai irreversible', () => {
    expect(nextAction({ status: 'succeeded', isActive: true, downSql: null, irreversibleReason: null })).toBeNull()
    expect(nextAction({ status: 'succeeded', isActive: true, downSql: 'x', irreversibleReason: 'DROP data' })).toBeNull()
  })

  it.each(['running', 'interrupted', 'needs_reconciliation', 'reconciled'] as const)('%s tidak punya aksi', (status) => {
    expect(nextAction({ status, ...reversible })).toBeNull()
  })
})
