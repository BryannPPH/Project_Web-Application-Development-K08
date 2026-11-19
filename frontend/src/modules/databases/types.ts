/*
 * FILE INI UNTUK APA (databases/types.ts)
 * GUIDEBOOK: §2.8
 * Tipe TypeScript untuk data dari GET /api/v1/databases (bentuknya sama dengan
 * DTO AccessibleDatabase di backend) + helper label akses "View Only"/"View + Edit".
 */
export type AccessLevel = 'view_only' | 'view_edit'
export type DatabaseEngine = 'postgresql' | 'mysql'

export interface DatabaseCapabilities {
  viewer: boolean
  transactionalMigration: boolean
  transactionalDdl: boolean
}

export interface AccessibleDatabase {
  id: string
  name: string
  databaseName: string
  engine: DatabaseEngine
  capabilities: DatabaseCapabilities
  accessLevel: AccessLevel
}

export function accessLevelLabel(level: AccessLevel) {
  return level === 'view_edit' ? 'View + Edit' : 'View Only'
}

export function databaseEngineLabel(engine: DatabaseEngine) {
  return engine === 'mysql' ? 'MySQL' : 'PostgreSQL'
}
