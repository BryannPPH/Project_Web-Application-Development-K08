import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { vi } from 'vitest'

import type { Principal } from '@/modules/auth/types'
import type { MigrationEntry } from '@/modules/migrations/types'
import { authQueryKeys } from '@/modules/auth/use-current-user'
import { LocationProbe } from '@/test/LocationProbe'

export function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    status,
  })
}

export function errorResponse(status: number, code: string, message = code) {
  return jsonResponse({ error: { code, message, requestId: 'request-test' } }, status)
}

type Handler = (url: URL, init: RequestInit | undefined) => Response | undefined

// mockFetch memetakan request ke handler; request tak terduga membuat test gagal.
export function mockFetch(handler: Handler) {
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    const url = new URL(String(input), 'http://localhost')
    const response = handler(url, init)
    if (!response) {
      return Promise.reject(new Error(`Unexpected request: ${init?.method ?? 'GET'} ${url.pathname}${url.search}`))
    }
    return Promise.resolve(response)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

interface RenderOptions {
  route: string
  path: string
  user?: Principal
  extraRoutes?: ReactNode
}

export function renderRoute(element: ReactNode, { route, path, user, extraRoutes }: RenderOptions) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  if (user) {
    queryClient.setQueryData(authQueryKeys.currentUser, user)
  }
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>
        <Routes>
          <Route
            element={
              <>
                {element}
                <LocationProbe />
              </>
            }
            path={path}
          />
          {extraRoutes}
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return queryClient
}

export const adminUser: Principal = { id: 'admin-1', username: 'admin', platformRole: 'platform_admin' }
export const regularUser: Principal = { id: 'user-1', username: 'operator', platformRole: 'user' }

// senjaImage mencari ilustrasi maskot berdasarkan nama file asetnya
// (mis. 'sleeping' untuk senja-sleeping.png).
export function senjaImage(pose: string) {
  return document.querySelector(`img[src*="senja-${pose}"]`)
}

export function migrationEntry(overrides: Partial<MigrationEntry> = {}): MigrationEntry {
  return {
    id: 'm1',
    databaseId: 'db-1',
    version: 1,
    name: 'add_points',
    sql: 'ALTER TABLE customers ADD COLUMN points int;',
    status: 'draft',
    createdBy: 'admin',
    createdByUsername: 'admin',
    executedBy: null,
    executedByUsername: null,
    createdAt: '2026-10-20T09:00:00Z',
    startedAt: null,
    finishedAt: null,
    errorMessage: null,
    canEdit: true,
    ...overrides,
  }
}
