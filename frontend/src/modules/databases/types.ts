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
