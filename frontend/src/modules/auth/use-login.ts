import { useMutation, useQueryClient } from '@tanstack/react-query'

import { login } from '@/modules/auth/api'
import { authQueryKeys } from '@/modules/auth/use-current-user'

export function useLogin() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: login,
    onSuccess: ({ user }) => {
      queryClient.setQueryData(authQueryKeys.currentUser, user)
    },
  })
}
