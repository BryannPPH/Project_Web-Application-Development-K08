export type PlatformRole = 'user' | 'platform_admin'

export interface Principal {
  id: string
  username: string
  platformRole: PlatformRole
}

export interface LoginCredentials {
  username: string
  password: string
}

export interface LoginResult {
  user: Principal
  csrfToken: string
  absoluteExpiresAt: string
}
