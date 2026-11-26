import { describe, expect, it } from 'vitest'

import { availableActions, nextAction } from '@/modules/migrations/actions'

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

describe('availableActions', () => {
  it('offers checkpoint resume and approved rollback for a reversible batch partial', () => {
    const actions = availableActions({
      status: 'partial',
      executionMode: 'batch',
      downSql: 'UPDATE t SET value=value-1 WHERE id>$1 AND id<=$2',
      irreversibleReason: null,
      isActive: false,
    })

    expect(actions.map((action) => [action.action, action.approval])).toEqual([
      ['retry', 'apply'],
      ['rollback', 'rollback'],
    ])
  })

  it('does not offer rollback for irreversible or non-batch partial migrations', () => {
    expect(availableActions({ status: 'partial', executionMode: 'single', downSql: 'x', irreversibleReason: null })).toHaveLength(1)
    expect(availableActions({ status: 'partial', executionMode: 'batch', downSql: null, irreversibleReason: 'irreversible' })).toHaveLength(1)
  })
})
