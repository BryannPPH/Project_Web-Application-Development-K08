import type { PlatformRole } from '@/modules/auth/types'
import type { AccessLevel, DatabaseEngine } from '@/modules/databases/types'

export interface AdminUser {
  id: string
  username: string
  platformRole: PlatformRole
  isActive: boolean
  createdAt: string
  updatedAt: string
}

export interface AdminGroup {
  id: string
  name: string
  description: string | null
  memberCount: number
  createdAt: string
  updatedAt: string
}

export interface GroupMember {
  userId: string
  username: string
  platformRole: PlatformRole
  isActive: boolean
  addedAt: string
}

export interface GroupGrant {
  groupId: string
  groupName: string
  databaseId: string
  databaseName: string
  accessLevel: AccessLevel
  createdAt: string
  updatedAt: string
}

export type SSLMode = 'disable' | 'allow' | 'prefer' | 'require' | 'verify-ca' | 'verify-full'

export const sslModes: SSLMode[] = ['disable', 'allow', 'prefer', 'require', 'verify-ca', 'verify-full']

// ManagedDatabase tidak pernah membawa password/ciphertext; hanya hasCredential.
export interface ManagedDatabase {
  id: string
  name: string
  engine: DatabaseEngine
  host: string
  port: number
  databaseName: string
  username: string
  sslMode: SSLMode
  isActive: boolean
  hasCredential: boolean
  createdAt: string
  updatedAt: string
}

export interface ManagedDatabaseInput {
  name: string
  engine: DatabaseEngine
  host: string
  port: number
  databaseName: string
  username: string
  password?: string
  sslMode: SSLMode
  isActive: boolean
}

export interface ConnectionTestResult {
  success: boolean
  reason?: string
  latencyMs: number
}
