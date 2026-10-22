import { useEffect } from 'react'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from 'react-router-dom'

import { queryClient } from '@/app/query-client'
import { router } from '@/app/router'
import { setUnauthorizedHandler } from '@/lib/api-client'

export function App() {
  useEffect(
    () =>
      setUnauthorizedHandler(() => {
        queryClient.removeQueries()

        if (window.location.pathname !== '/login') {
          const from = `${window.location.pathname}${window.location.search}${window.location.hash}`
          void router.navigate('/login', { replace: true, state: { from } })
        }
      }),
    [],
  )

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

