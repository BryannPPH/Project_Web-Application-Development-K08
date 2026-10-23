export type AccessLevel = 'view_only' | 'view_edit'

export interface AccessibleDatabase {
  id: string
  name: string
  databaseName: string
  accessLevel: AccessLevel
}

export function accessLevelLabel(level: AccessLevel) {
  return level === 'view_edit' ? 'View + Edit' : 'View Only'
}
