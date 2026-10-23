import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { clearCsrfToken, setCsrfToken } from '@/lib/csrf-token'
import { AdminDatabasesPage } from '@/modules/admin/AdminDatabasesPage'
import type { ManagedDatabase } from '@/modules/admin/types'
import { adminUser, jsonResponse, mockFetch, renderRoute } from '@/test/platform-test-utils'

const stored: ManagedDatabase = {
  id: 'db-1',
  name: 'Warehouse',
  host: 'db.internal',
  port: 5432,
  databaseName: 'warehouse',
  username: 'reader',
  sslMode: 'require',
  isActive: true,
  hasCredential: true,
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-01T00:00:00Z',
}

beforeEach(() => {
  setCsrfToken('csrf-test')
})

afterEach(() => {
  cleanup()
  clearCsrfToken()
  vi.unstubAllGlobals()
})

function renderPage() {
  renderRoute(<AdminDatabasesPage />, { route: '/admin/databases', path: '/admin/databases', user: adminUser })
}

describe('AdminDatabasesPage', () => {
  it('tidak pernah mem-prefill password saat mengubah koneksi', async () => {
    mockFetch((url) => (url.pathname === '/api/v1/admin/databases' ? jsonResponse({ databases: [stored] }) : undefined))
    renderPage()

    expect(await screen.findByText('Tersimpan (terenkripsi)')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Ubah' }))

    const form = await screen.findByRole('form', { name: 'Ubah koneksi database' })
    expect(within(form).getByLabelText('Host')).toHaveValue('db.internal')
    const password = within(form).getByLabelText('Password')
    expect(password).toHaveValue('')
    expect(password).toHaveAttribute('type', 'password')
    expect(password).toHaveAttribute('placeholder', 'Kosongkan bila tidak diubah')
  })

  it('memvalidasi form sebelum mengirim request', async () => {
    const fetchMock = mockFetch((url) =>
      url.pathname === '/api/v1/admin/databases' ? jsonResponse({ databases: [] }) : undefined,
    )
    renderPage()

    expect(await screen.findByText('Belum ada koneksi database.')).toBeInTheDocument()
    const form = screen.getByRole('form', { name: 'Tambah koneksi database' })
    fireEvent.change(within(form).getByLabelText('Nama koneksi'), { target: { value: 'Target' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Tambah koneksi' }))

    expect(await within(form).findByRole('alert')).toHaveTextContent('wajib diisi')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('mengirim koneksi baru dengan CSRF token', async () => {
    let created: unknown
    const fetchMock = mockFetch((url, init) => {
      if (url.pathname === '/api/v1/admin/databases' && (init?.method ?? 'GET') === 'GET') {
        return jsonResponse({ databases: [] })
      }
      if (url.pathname === '/api/v1/admin/databases' && init?.method === 'POST') {
        created = JSON.parse(String(init.body))
        return jsonResponse({ database: stored }, 201)
      }
      return undefined
    })
    renderPage()

    const form = await screen.findByRole('form', { name: 'Tambah koneksi database' })
    const fill = (label: string, value: string) =>
      fireEvent.change(within(form).getByLabelText(label), { target: { value } })
    fill('Nama koneksi', 'Warehouse')
    fill('Host', 'db.internal')
    fill('Nama database', 'warehouse')
    fill('Username', 'reader')
    fill('Password', 'super-secret')
    fireEvent.click(within(form).getByRole('button', { name: 'Tambah koneksi' }))

    await waitFor(() => expect(created).toBeDefined())
    expect(created).toMatchObject({ name: 'Warehouse', password: 'super-secret', port: 5432, sslMode: 'require' })
    const postCall = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(new Headers(postCall?.[1]?.headers).get('X-CSRF-Token')).toBe('csrf-test')
  })
})
