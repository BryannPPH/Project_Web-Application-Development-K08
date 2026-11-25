export type MigrationStatus =
  | 'draft'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'interrupted'
  | 'needs_reconciliation'
  | 'reconciled'
  | 'rolled_back'
  | 'partial'
  | 'partially_reverted'

export interface MigrationEntry {
  id: string
  databaseId: string
  version: number
  /** Milestone 2: naik setiap nama/SQL diedit. Approval berlaku per revisi. */
  revision: number
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
  /** Milestone 2 (Person B). Opsional supaya payload lama tetap valid. */
  isActive?: boolean
  executionMode?: ExecutionMode
  latestExecutionId?: string | null
  downSql?: string | null
  irreversibleReason?: string | null
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

// ---------- Approval (Milestone 2 - Person A) ----------

export type ApprovalAction = 'apply' | 'rollback' | 'forward'
export type ApprovalDecision = 'approved' | 'rejected'
/** approved = boleh jalan; pending = menunggu leader; rejected = ada yang menolak;
 *  blocked = ada group ber-akses yang belum punya leader. */
export type ApprovalStatus = 'approved' | 'pending' | 'rejected' | 'blocked'

export interface ApprovalGroup {
  groupId: string
  groupName: string
  accessLevel: 'view_only' | 'view_edit'
  leader: { userId: string; username: string } | null
  decision: ApprovalDecision | null
  note: string | null
  decidedAt: string | null
}

export interface ApprovalReview {
  migrationId: string
  databaseId: string
  action: ApprovalAction
  revision: number
  status: ApprovalStatus
  groups: ApprovalGroup[]
  approvedCount: number
  requiredCount: number
  canDecide: boolean
  myDecision: ApprovalDecision | null
}

export interface ApprovalInput {
  action: ApprovalAction
  revision: number
  decision: ApprovalDecision
  note?: string
}

// ---------- Execution, job & batch (Milestone 2 - Person B/C) ----------

export type ExecutionMode = 'single' | 'batch'
export type ExecutionAction = 'apply' | 'retry' | 'rollback' | 'forward'
export type JobStatus =
  | 'queued'
  | 'running'
  | 'retrying'
  | 'succeeded'
  | 'failed'
  | 'partial'
  | 'partially_reverted'
  | 'needs_reconciliation'

export interface BatchProgress {
  executionId: string
  committedBatches: number
  committedRows: number
  lastCursor: number
  status: string
}

export interface MigrationJob {
  jobId: string
  databaseId: string
  migrationId: string
  revision: number
  action: ExecutionAction
  executionMode: ExecutionMode
  actor: string
  status: JobStatus
  retryCount: number
  executionId?: string
  createdAt: string
  startedAt?: string
  finishedAt?: string
  errorCode?: string
  errorMessage?: string
  batchProgress?: BatchProgress
}

export interface ExecutionAttempt {
  executionId: string
  migrationId: string
  version: number
  revision: number
  action: ExecutionAction
  actorUsername: string
  requestedAt: string
  relatedExecutionId: string | null
  status: string
  errorMessage: string | null
}

export interface ExecutionHistoryPage {
  items: ExecutionAttempt[]
  limit: number
  offset: number
  total: number
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
