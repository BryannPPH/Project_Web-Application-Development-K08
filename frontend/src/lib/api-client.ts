import { clearCsrfToken, getCsrfToken } from '@/lib/csrf-token'

const API_PREFIX = '/api/v1'
const mutationMethods = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

type UnauthorizedHandler = () => void

let unauthorizedHandler: UnauthorizedHandler | null = null

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId?: string

  constructor(
    status: number,
    code: string,
    message: string,
    requestId?: string,
    options?: ErrorOptions,
  ) {
    super(message, options)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.requestId = requestId
  }
}

export interface ApiRequestOptions
  extends Omit<RequestInit, 'body' | 'headers'> {
  body?: BodyInit | null
  csrf?: boolean
  headers?: HeadersInit
  json?: unknown
  notifyUnauthorized?: boolean
}

export function setUnauthorizedHandler(handler: UnauthorizedHandler | null) {
  unauthorizedHandler = handler

  return () => {
    if (unauthorizedHandler === handler) {
      unauthorizedHandler = null
    }
  }
}

function isErrorResponse(
  value: unknown,
): value is { error: { code: string; message: string; requestId?: string } } {
  if (!value || typeof value !== 'object' || !('error' in value)) {
    return false
  }

  const error = value.error
  return (
    !!error &&
    typeof error === 'object' &&
    'code' in error &&
    typeof error.code === 'string' &&
    'message' in error &&
    typeof error.message === 'string' &&
    (!('requestId' in error) || typeof error.requestId === 'string')
  )
}

async function readBody(response: Response) {
  const text = await response.text()

  if (text === '') {
    return undefined
  }

  try {
    return JSON.parse(text) as unknown
  } catch (error) {
    throw new ApiError(
      response.status,
      'INVALID_RESPONSE',
      'Server mengembalikan respons yang tidak valid.',
      undefined,
      { cause: error },
    )
  }
}

function createApiError(response: Response, body: unknown) {
  if (isErrorResponse(body)) {
    return new ApiError(
      response.status,
      body.error.code,
      body.error.message,
      body.error.requestId,
    )
  }

  return new ApiError(
    response.status,
    'HTTP_ERROR',
    'Permintaan tidak dapat diproses.',
  )
}

export async function apiRequest<T>(
  path: string,
  options: ApiRequestOptions = {},
): Promise<T> {
  const {
    body,
    csrf,
    headers: initialHeaders,
    json,
    notifyUnauthorized = true,
    ...requestOptions
  } = options

  if (body !== undefined && json !== undefined) {
    throw new Error('Gunakan body atau json, bukan keduanya.')
  }

  const method = (requestOptions.method ?? 'GET').toUpperCase()
  const headers = new Headers(initialHeaders)
  const requiresCsrf = csrf ?? mutationMethods.has(method)

  headers.set('Accept', 'application/json')

  if (json !== undefined) {
    headers.set('Content-Type', 'application/json')
  }

  if (requiresCsrf) {
    const token = getCsrfToken()
    if (!token) {
      throw new ApiError(
        0,
        'CSRF_TOKEN_MISSING',
        'Token keamanan belum tersedia. Muat ulang halaman dan coba lagi.',
      )
    }
    headers.set('X-CSRF-Token', token)
  }

  let response: Response
  try {
    response = await fetch(`${API_PREFIX}${path}`, {
      ...requestOptions,
      body: json === undefined ? body : JSON.stringify(json),
      credentials: 'include',
      headers,
      method,
    })
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw error
    }

    throw new ApiError(
      0,
      'NETWORK_ERROR',
      'Tidak dapat terhubung ke server.',
      undefined,
      { cause: error },
    )
  }

  const responseBody = await readBody(response)

  if (!response.ok) {
    if (response.status === 401) {
      clearCsrfToken()
      if (notifyUnauthorized) {
        unauthorizedHandler?.()
      }
    }

    throw createApiError(response, responseBody)
  }

  return responseBody as T
}
