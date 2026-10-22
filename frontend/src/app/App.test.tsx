import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from '@/app/App'
import { queryClient } from '@/app/query-client'

beforeEach(() => {
  queryClient.clear()
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({
          error: {
            code: 'UNAUTHENTICATED',
            message: 'authentication required',
            requestId: 'request-app',
          },
        }),
        {
          headers: { 'Content-Type': 'application/json' },
          status: 401,
        },
      ),
    ),
  )
})

afterEach(() => {
  queryClient.clear()
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('menampilkan login ketika sesi tidak tersedia', async () => {
    render(<App />)

    expect(
      await screen.findByRole('heading', { name: 'Masuk ke akun Anda' }),
    ).toBeInTheDocument()
  })
})

