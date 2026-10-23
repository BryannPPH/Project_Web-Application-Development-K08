import { keepPreviousData, useQuery } from '@tanstack/react-query'

import { ApiError, apiRequest } from '@/lib/api-client'
import type {
  RowsPage,
  SchemaSummary,
  TableDetail,
  TableSummary,
} from '@/modules/viewer/types'

export const DEFAULT_PAGE_SIZE = 50
export const MAX_PAGE_SIZE = 100

// Identifier schema/table dapat berisi karakter apa pun, jadi selalu di-encode.
function basePath(databaseId: string) {
  return `/databases/${encodeURIComponent(databaseId)}`
}

function tablePath(databaseId: string, schema: string, table: string) {
  return `${basePath(databaseId)}/schemas/${encodeURIComponent(schema)}/tables/${encodeURIComponent(table)}`
}

export const viewerQueryKeys = {
  schemas: (databaseId: string) => ['viewer', databaseId, 'schemas'] as const,
  tables: (databaseId: string, schema: string) =>
    ['viewer', databaseId, 'schemas', schema, 'tables'] as const,
  detail: (databaseId: string, schema: string, table: string) =>
    ['viewer', databaseId, 'schemas', schema, 'tables', table, 'detail'] as const,
  rows: (databaseId: string, schema: string, table: string, limit: number, offset: number) =>
    ['viewer', databaseId, 'schemas', schema, 'tables', table, 'rows', limit, offset] as const,
}

function retryServerErrors(failureCount: number, error: unknown) {
  if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
    return false
  }
  return failureCount < 1
}

export function useSchemas(databaseId: string) {
  return useQuery({
    queryKey: viewerQueryKeys.schemas(databaseId),
    queryFn: async ({ signal }) =>
      (await apiRequest<{ schemas: SchemaSummary[] }>(`${basePath(databaseId)}/schemas`, { signal }))
        .schemas,
    retry: retryServerErrors,
  })
}

export function useTables(databaseId: string, schema: string | undefined) {
  return useQuery({
    enabled: !!schema,
    queryKey: viewerQueryKeys.tables(databaseId, schema ?? ''),
    queryFn: async ({ signal }) =>
      (
        await apiRequest<{ tables: TableSummary[] }>(
          `${basePath(databaseId)}/schemas/${encodeURIComponent(schema ?? '')}/tables`,
          { signal },
        )
      ).tables,
    retry: retryServerErrors,
  })
}

export function useTableDetail(databaseId: string, schema: string, table: string) {
  return useQuery({
    queryKey: viewerQueryKeys.detail(databaseId, schema, table),
    queryFn: ({ signal }) =>
      apiRequest<TableDetail>(tablePath(databaseId, schema, table), { signal }),
    retry: retryServerErrors,
  })
}

export function useTableRows(
  databaseId: string,
  schema: string,
  table: string,
  limit: number,
  offset: number,
) {
  return useQuery({
    queryKey: viewerQueryKeys.rows(databaseId, schema, table, limit, offset),
    queryFn: ({ signal }) => {
      const query = new URLSearchParams({ limit: String(limit), offset: String(offset) })
      return apiRequest<RowsPage>(`${tablePath(databaseId, schema, table)}/rows?${query}`, { signal })
    },
    placeholderData: keepPreviousData,
    retry: retryServerErrors,
  })
}

// parsePageParams membaca limit/offset dari URL dengan batas yang sama seperti backend.
export function parsePageParams(searchParams: URLSearchParams) {
  const rawLimit = Number(searchParams.get('limit') ?? DEFAULT_PAGE_SIZE)
  const rawOffset = Number(searchParams.get('offset') ?? 0)
  const limit =
    Number.isInteger(rawLimit) && rawLimit >= 1 && rawLimit <= MAX_PAGE_SIZE
      ? rawLimit
      : DEFAULT_PAGE_SIZE
  const offset = Number.isInteger(rawOffset) && rawOffset >= 0 ? rawOffset : 0
  return { limit, offset }
}
