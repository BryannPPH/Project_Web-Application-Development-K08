import type { DatabaseEngine } from '@/modules/databases/types'

export interface SchemaRef {
  databaseId: string
  schema: string
}

export type ChangeKind =
  | 'create_table'
  | 'drop_table'
  | 'add_column'
  | 'drop_column'
  | 'alter_column'
  | 'add_constraint'
  | 'drop_constraint'
  | 'create_index'
  | 'drop_index'

export interface SchemaChange {
  id: string
  kind: ChangeKind
  table: string
  object?: string
  description: string
  before?: string
  after?: string
  destructive: boolean
  warning?: string
  sql: string[]
}

export interface SchemaDiffResult {
  engine: DatabaseEngine
  source: SchemaRef
  target: SchemaRef
  identical: boolean
  changes: SchemaChange[]
  sql: string
  summary: { total: number; destructive: number; tables: number }
  executable: boolean
  notes: string[]
}
