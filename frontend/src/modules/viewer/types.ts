export interface SchemaSummary {
  name: string
  owner: string
}

export type TableType =
  | 'table'
  | 'partitioned_table'
  | 'view'
  | 'materialized_view'
  | 'foreign_table'
  | 'unknown'

export interface TableSummary {
  schema: string
  name: string
  type: TableType
  estimatedRows: number
}

export interface ColumnInfo {
  name: string
  position: number
  dataType: string
  nullable: boolean
  default: string | null
  identity?: string
  generated?: string
}

export interface ConstraintInfo {
  name: string
  type: 'primary_key' | 'foreign_key' | 'unique' | 'check' | 'exclusion' | 'other'
  columns: string[]
  definition: string
  references?: { schema: string; table: string; columns: string[] }
}

export interface IndexInfo {
  name: string
  unique: boolean
  primary: boolean
  method: string
  columns: string[]
  definition: string
}

export interface TableDetail {
  table: TableSummary
  columns: ColumnInfo[]
  constraints: ConstraintInfo[]
  indexes: IndexInfo[]
}

export interface RowsPage {
  columns: { name: string; dataType: string }[]
  rows: unknown[][]
  limit: number
  offset: number
  hasMore: boolean
  orderedBy: string[]
  truncated: boolean
}

export const tableTypeLabels: Record<TableType, string> = {
  table: 'Table',
  partitioned_table: 'Partitioned',
  view: 'View',
  materialized_view: 'Materialized view',
  foreign_table: 'Foreign table',
  unknown: 'Lainnya',
}

export const constraintTypeLabels: Record<ConstraintInfo['type'], string> = {
  primary_key: 'PRIMARY KEY',
  foreign_key: 'FOREIGN KEY',
  unique: 'UNIQUE',
  check: 'CHECK',
  exclusion: 'EXCLUDE',
  other: 'Lainnya',
}
