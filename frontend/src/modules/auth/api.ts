import { ApiError, apiRequest } from '@/lib/api-client'
import {
  clearCsrfToken,
  getCsrfToken,
  setCsrfToken,
} from '@/lib/csrf-token'
import type {
  LoginCredentials,
  LoginResult,
  PlatformRole,
  Principal,
} from '@/modules/auth/types'

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function parsePlatformRole(value: unknown): PlatformRole {
  if (value === 'user' || value === 'platform_admin') {
    return value
  }

  throw invalidResponse()
}

function parsePrincipal(value: unknown): Principal {
  if (
    !isRecord(value) ||
    typeof value.id !== 'string' ||
    typeof value.username !== 'string'
  ) {
    throw invalidResponse()
  }

  return {
    id: value.id,
    username: value.username,
    platformRole: parsePlatformRole(value.platformRole),
  }
}

function invalidResponse() {
  return new ApiError(
    200,
    'INVALID_RESPONSE',
    'Server mengembalikan respons autentikasi yang tidak valid.',
  )
}

function parseCsrfToken(value: unknown) {
  if (!isRecord(value) || typeof value.csrfToken !== 'string') {
    throw invalidResponse()
  }

  return value.csrfToken
}

export async function login(credentials: LoginCredentials) {
  const response = await apiRequest<unknown>('/auth/login', {
    csrf: false,
    json: credentials,
    method: 'POST',
    notifyUnauthorized: false,
  })

  if (!isRecord(response) || typeof response.absoluteExpiresAt !== 'string') {
    throw invalidResponse()
  }

  const result: LoginResult = {
    user: parsePrincipal(response.user),
    csrfToken: parseCsrfToken(response),
    absoluteExpiresAt: response.absoluteExpiresAt,
  }
  setCsrfToken(result.csrfToken)

  return result
}

export async function refreshCsrfToken(signal?: AbortSignal) {
  const response = await apiRequest<unknown>('/auth/csrf', {
    notifyUnauthorized: false,
    signal,
  })
  const token = parseCsrfToken(response)
  setCsrfToken(token)

  return token
}

export async function getCurrentUser(signal?: AbortSignal) {
  try {
    const response = await apiRequest<unknown>('/auth/me', {
      notifyUnauthorized: false,
      signal,
    })

    if (!isRecord(response)) {
      throw invalidResponse()
    }

    const user = parsePrincipal(response.user)
    if (!getCsrfToken()) {
      await refreshCsrfToken(signal)
    }

    return user
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      clearCsrfToken()
      return null
    }
    throw error
  }
}

export async function logout() {
  await apiRequest<unknown>('/auth/logout', { method: 'POST' })
  clearCsrfToken()
}
