export type MigrationStatus =
  | 'draft'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'interrupted'

export interface MigrationEntry {
  id: string
  databaseId: string
  version: number
  name: string
  sql: string
  status: MigrationStatus
  createdBy: string
  executedBy: string | null
  createdAt: string
  startedAt: string | null
  finishedAt: string | null
  errorMessage: string | null
  canEdit: boolean
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
