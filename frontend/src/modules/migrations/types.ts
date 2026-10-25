export type MigrationStatus =
  | 'draft'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'interrupted'
  | 'needs_reconciliation'
  | 'reconciled'

export interface MigrationEntry {
  id: string
  databaseId: string
  version: number
  name: string
  sql: string
  status: MigrationStatus
  createdBy: string
  createdByUsername: string
  executedBy: string | null
  executedByUsername: string | null
  createdAt: string
  startedAt: string | null
  finishedAt: string | null
  errorMessage: string | null
  reconciledFrom: MigrationStatus | null
  reconciledBy: string | null
  reconciledByUsername: string | null
  reconciledAt: string | null
  reconciliationNote: string | null
  canEdit: boolean
  canReconcile: boolean
}

export interface ReconciliationInput {
  inspectedTarget: true
  note: string
}

export interface MigrationPage {
  items: MigrationEntry[]
  limit: number
  offset: number
  total: number
  canEdit: boolean
}

export interface DraftInput {
  name: string
  sql: string
}

// ---------- Query execution plan analysis (bonus) ----------

export interface PlanNode {
  nodeType: string
  relation?: string
  alias?: string
  index?: string
  joinType?: string
  condition?: string
  filter?: string
  startupCost?: number
  totalCost?: number
  planRows?: number
  actualStartupMs?: number
  actualTotalMs?: number
  actualRows?: number
  actualLoops?: number
  children?: PlanNode[]
}

export interface PlanSummary {
  totalCost?: number
  estimatedRows?: number
  actualRows?: number
  planningMs?: number
  executionMs?: number
  scanTypes: string[]
  indexes: string[]
  joinTypes: string[]
  relations: string[]
  warnings: string[]
}

export type StatementKind = 'plan' | 'executed' | 'error'

export interface StatementPlan {
  index: number
  sql: string
  kind: StatementKind
  note?: string
  error?: string
  plan?: PlanNode
  summary?: PlanSummary
  raw?: string
}

export interface ExplainResult {
  engine: 'postgresql' | 'mysql'
  analyze: boolean
  rolledBack: boolean
  statements: StatementPlan[]
}
