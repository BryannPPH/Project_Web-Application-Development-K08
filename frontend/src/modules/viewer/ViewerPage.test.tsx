import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ViewerPage } from '@/modules/viewer/ViewerPage'
import { errorResponse, jsonResponse, mockFetch, regularUser, renderRoute } from '@/test/platform-test-utils'

const database = { id: 'db-1', name: 'Warehouse', databaseName: 'warehouse', accessLevel: 'view_only' }
const tablePath = '/api/v1/databases/db-1/schemas/public/tables/customers'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function standardHandler(rowsFor: (offset: number) => Response) {
  return (url: URL) => {
    switch (url.pathname) {
      case '/api/v1/databases/db-1':
        return jsonResponse({ database })
      case '/api/v1/databases':
        return jsonResponse({ databases: [database] })
      case '/api/v1/databases/db-1/schemas':
        return jsonResponse({ schemas: [{ name: 'public', owner: 'owner' }] })
      case '/api/v1/databases/db-1/schemas/public/tables':
        return jsonResponse({
          tables: [{ schema: 'public', name: 'customers', type: 'table', estimatedRows: 120 }],
        })
      case tablePath:
        return jsonResponse({
          table: { schema: 'public', name: 'customers', type: 'table', estimatedRows: 120 },
          columns: [
            { name: 'id', position: 1, dataType: 'integer', nullable: false, default: "nextval('customers_id_seq'::regclass)" },
            { name: 'email', position: 2, dataType: 'text', nullable: false, default: null },
          ],
          constraints: [
            { name: 'customers_pkey', type: 'primary_key', columns: ['id'], definition: 'PRIMARY KEY (id)' },
            { name: 'customers_email_key', type: 'unique', columns: ['email'], definition: 'UNIQUE (email)' },
          ],
          indexes: [
            {
              name: 'customers_pkey',
              unique: true,
              primary: true,
              method: 'btree',
              columns: ['id'],
              definition: 'CREATE UNIQUE INDEX customers_pkey ON public.customers USING btree (id)',
            },
          ],
        })
      case `${tablePath}/rows`:
        return rowsFor(Number(url.searchParams.get('offset')))
      default:
        return undefined
    }
  }
}

function rowsPage(offset: number, hasMore: boolean) {
  return jsonResponse({
    columns: [
      { name: 'id', dataType: 'integer' },
      { name: 'email', dataType: 'text' },
    ],
    rows: [
      [offset + 1, `c${offset + 1}@example.com`],
      [offset + 2, null],
    ],
    limit: 50,
    offset,
    hasMore,
    orderedBy: ['id'],
    truncated: false,
  })
}

function renderViewer(route: string) {
  renderRoute(<ViewerPage />, {
    route,
    path: '/databases/:databaseId/schemas/:schema/tables/:table',
    user: regularUser,
  })
}

describe('ViewerPage', () => {
  it('menampilkan struktur table: column, constraint, index', async () => {
    mockFetch(standardHandler((offset) => rowsPage(offset, false)))
    renderViewer('/databases/db-1/schemas/public/tables/customers')

    expect(screen.getByText('Memeriksa akses database…')).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Warehouse' })).toBeInTheDocument()
    expect(await screen.findByText('customers_email_key')).toBeInTheDocument()
    expect(screen.getByText('UNIQUE')).toBeInTheDocument()
    expect(screen.getByText('CREATE UNIQUE INDEX customers_pkey ON public.customers USING btree (id)')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'customers' })).toHaveAttribute('aria-current', 'page')
  })

  it('menampilkan data dan mempertahankan database saat pindah halaman', async () => {
    const fetchMock = mockFetch(standardHandler((offset) => rowsPage(offset, offset === 0)))
    renderViewer('/databases/db-1/schemas/public/tables/customers?tab=data')

    expect(await screen.findByText('c1@example.com')).toBeInTheDocument()
    expect(screen.getByText('NULL')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sebelumnya' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: 'Berikutnya' }))

    expect(await screen.findByText('c51@example.com')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent(
      '/databases/db-1/schemas/public/tables/customers?tab=data&limit=50&offset=50',
    )
    expect(screen.getByRole('button', { name: 'Berikutnya' })).toBeDisabled()
    const rowRequests = fetchMock.mock.calls
      .map(([input]) => new URL(String(input), 'http://localhost'))
      .filter((url) => url.pathname === `${tablePath}/rows`)
      .map((url) => url.searchParams.get('offset'))
    expect(rowRequests).toEqual(['0', '50'])
  })

  it('menampilkan empty state data', async () => {
    mockFetch(
      standardHandler(() =>
        jsonResponse({ columns: [], rows: [], limit: 50, offset: 0, hasMore: false, orderedBy: [], truncated: false }),
      ),
    )
    renderViewer('/databases/db-1/schemas/public/tables/customers?tab=data')

    expect(await screen.findByText('Table ini belum memiliki data.')).toBeInTheDocument()
  })

  it('menampilkan error saat target tidak tersedia', async () => {
    mockFetch(standardHandler(() => errorResponse(502, 'TARGET_UNAVAILABLE', 'target database is unavailable')))
    renderViewer('/databases/db-1/schemas/public/tables/customers?tab=data')

    // Error 5xx di-retry sekali oleh hook sebelum ditampilkan.
    expect(
      await screen.findByText('Database target tidak dapat dihubungi. Coba lagi nanti.', {}, { timeout: 4000 }),
    ).toBeInTheDocument()
  })

  it('menampilkan error ketika user tidak memiliki akses database', async () => {
    mockFetch((url) =>
      url.pathname === '/api/v1/databases/db-1' ? errorResponse(404, 'DATABASE_NOT_FOUND', 'database not found') : undefined,
    )
    renderViewer('/databases/db-1/schemas/public/tables/customers')

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('Database tidak ditemukan atau Anda tidak memiliki akses.'),
    )
  })
})
