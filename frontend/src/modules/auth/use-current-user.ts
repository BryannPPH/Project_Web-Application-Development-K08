import { useQuery } from '@tanstack/react-query'

import { ApiError } from '@/lib/api-client'
import { getCurrentUser } from '@/modules/auth/api'

export const authQueryKeys = {
  all: ['auth'] as const,
  currentUser: ['auth', 'current-user'] as const,
}

export function useCurrentUser() {
  return useQuery({
    queryKey: authQueryKeys.currentUser,
    queryFn: ({ signal }) => getCurrentUser(signal),
    retry: (failureCount, error) => {
      if (error instanceof ApiError && error.status > 0 && error.status < 500) {
        return false
      }
      return failureCount < 1
    },
    staleTime: 30_000,
  })
}
