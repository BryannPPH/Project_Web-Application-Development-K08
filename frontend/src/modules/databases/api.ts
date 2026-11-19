import { useQuery } from '@tanstack/react-query'

import { ApiError, apiRequest } from '@/lib/api-client'
import type { AccessibleDatabase } from '@/modules/databases/types'

// Query key = "alamat" data di cache React Query. Admin api.ts meng-invalidate
// key 'databases' setelah grant berubah supaya daftar ini ter-refresh.
export const databaseQueryKeys = {
  all: ['databases'] as const,
  accessible: ['databases', 'accessible'] as const,
  detail: (id: string) => ['databases', 'detail', id] as const,
}

// Daftar ini hanya berisi database yang di-grant melalui group. Backend tetap
// memeriksa ulang akses pada setiap request viewer.
export async function listAccessibleDatabases(signal?: AbortSignal) {
  const response = await apiRequest<{ databases: AccessibleDatabase[] }>('/databases', { signal })
  return response.databases
}

// Detail 1 database (nama + level akses). encodeURIComponent: ID aman ditaruh di URL.
export async function getAccessibleDatabase(id: string, signal?: AbortSignal) {
  const response = await apiRequest<{ database: AccessibleDatabase }>(
    `/databases/${encodeURIComponent(id)}`,
    { signal },
  )
  return response.database
}

// Error 4xx tidak di-retry agar state forbidden/not-found tampil segera.
function retryServerErrors(failureCount: number, error: unknown) {
  // 4xx (403/404) = memang tidak boleh -> jangan coba lagi. 5xx -> coba ulang 1x.
  if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
    return false
  }
  return failureCount < 1
}

// Hook untuk DatabaseListPage. Output: { data, isLoading, error, refetch, ... }.
export function useAccessibleDatabases() {
  return useQuery({
    queryKey: databaseQueryKeys.accessible,
    queryFn: ({ signal }) => listAccessibleDatabases(signal),
    retry: retryServerErrors,
  })
}

// Hook untuk ViewerPage (judul + badge akses database yang sedang dibuka).
export function useAccessibleDatabase(id: string) {
  return useQuery({
    queryKey: databaseQueryKeys.detail(id),
    queryFn: ({ signal }) => getAccessibleDatabase(id, signal),
    retry: retryServerErrors,
  })
}
