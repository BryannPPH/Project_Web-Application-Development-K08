import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { clearCsrfToken, setCsrfToken } from '@/lib/csrf-token'
import { AdminUsersPage } from '@/modules/admin/AdminUsersPage'
import { adminUser, jsonResponse, mockFetch, renderRoute } from '@/test/platform-test-utils'

const longName = 'a'.repeat(64)

const users = [
  { id: 'user-1', username: 'admin', platformRole: 'platform_admin', isActive: true, createdAt: '2026-10-01T00:00:00Z', updatedAt: '2026-10-01T00:00:00Z' },
  { id: 'user-2', username: longName, platformRole: 'user', isActive: true, createdAt: '2026-10-02T00:00:00Z', updatedAt: '2026-10-02T00:00:00Z' },
]

beforeEach(() => {
  setCsrfToken('csrf-test')
})

afterEach(() => {
  cleanup()
  clearCsrfToken()
  vi.unstubAllGlobals()
})

describe('AdminUsersPage reset password', () => {
  it('menampilkan error tepat di bawah kolom password, lalu mengirim password yang valid', async () => {
    const fetchMock = mockFetch((url, init) => {
      if (url.pathname === '/api/v1/admin/users') return jsonResponse({ users })
      if (url.pathname === '/api/v1/admin/users/user-2/reset-password' && init?.method === 'POST') return jsonResponse({ user: users[1] })
      return undefined
    })
    renderRoute(<AdminUsersPage />, { route: '/admin/users', path: '/admin/users', user: adminUser })

    await screen.findByText(longName)
    fireEvent.click(screen.getAllByRole('button', { name: /Reset password/ })[1]!)

    const form = screen.getByRole('form', { name: `Reset password ${longName}` })
    const input = within(form).getByLabelText(`Password baru untuk ${longName}`)
    // Label panjang tetap satu baris (dipotong), nama lengkap tersedia lewat title.
    expect(within(form).getByText(`Password baru untuk ${longName}`)).toHaveAttribute('title', longName)

    fireEvent.change(input, { target: { value: 'pendek' } })
    fireEvent.submit(form)

    const error = within(form).getByRole('alert')
    expect(error).toHaveTextContent('Password baru minimal 8 karakter.')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input.getAttribute('aria-describedby')).toContain(error.id)

    // Mengetik ulang menghapus error; password valid dikirim ke backend.
    fireEvent.change(input, { target: { value: 'password-baru-123' } })
    expect(within(form).queryByRole('alert')).not.toBeInTheDocument()
    fireEvent.submit(form)

    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([request, init]) => String(request).endsWith('/admin/users/user-2/reset-password') && init?.method === 'POST')).toBe(true),
    )
    expect(await screen.findByText(`Password ${longName} diperbarui. Semua sesinya sudah dicabut.`)).toBeInTheDocument()
  })
})
