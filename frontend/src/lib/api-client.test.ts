import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  ApiError,
  apiRequest,
  setUnauthorizedHandler,
} from '@/lib/api-client'
import {
  clearCsrfToken,
  getCsrfToken,
  setCsrfToken,
} from '@/lib/csrf-token'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    status,
  })
}

afterEach(() => {
  clearCsrfToken()
  setUnauthorizedHandler(null)
  vi.unstubAllGlobals()
})

describe('apiRequest', () => {
  it('mengirim cookie dan CSRF pada mutation', async () => {
    const fetchMock = vi.fn<typeof fetch>()
    fetchMock.mockResolvedValue(jsonResponse({ id: 'resource-1' }))
    vi.stubGlobal('fetch', fetchMock)
    setCsrfToken('csrf-token')

    await apiRequest('/resources', {
      json: { name: 'Database utama' },
      method: 'POST',
    })

    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, options] = fetchMock.mock.calls[0]!
    const headers = new Headers(options?.headers)
    expect(url).toBe('/api/v1/resources')
    expect(options?.credentials).toBe('include')
    expect(headers.get('X-CSRF-Token')).toBe('csrf-token')
    expect(headers.get('Content-Type')).toBe('application/json')
  })

  it('mengubah error backend menjadi ApiError yang konsisten', async () => {
    const fetchMock = vi.fn<typeof fetch>()
    fetchMock.mockResolvedValue(
      jsonResponse(
        {
          error: {
            code: 'FORBIDDEN',
            message: 'access denied',
            requestId: 'request-1',
          },
        },
        403,
      ),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiRequest('/resources')).rejects.toEqual(
      expect.objectContaining({
        code: 'FORBIDDEN',
        message: 'access denied',
        requestId: 'request-1',
        status: 403,
      }),
    )
  })

  it('membersihkan CSRF dan memberi tahu aplikasi saat sesi berakhir', async () => {
    const fetchMock = vi.fn<typeof fetch>()
    fetchMock.mockResolvedValue(
      jsonResponse(
        {
          error: {
            code: 'UNAUTHENTICATED',
            message: 'authentication required',
            requestId: 'request-2',
          },
        },
        401,
      ),
    )
    vi.stubGlobal('fetch', fetchMock)
    setCsrfToken('csrf-token')
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)

    await expect(apiRequest('/databases')).rejects.toBeInstanceOf(ApiError)

    expect(getCsrfToken()).toBeNull()
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it('menolak mutation ketika token CSRF belum tersedia', async () => {
    const fetchMock = vi.fn<typeof fetch>()
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      apiRequest('/resources', { method: 'DELETE' }),
    ).rejects.toEqual(
      expect.objectContaining({ code: 'CSRF_TOKEN_MISSING', status: 0 }),
    )
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
