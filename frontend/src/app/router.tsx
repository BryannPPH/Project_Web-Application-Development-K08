import { createBrowserRouter } from 'react-router-dom'

import { FoundationPage } from '@/app/FoundationPage'
import { LoginPage } from '@/modules/auth/LoginPage'
import { ProtectedRoute } from '@/modules/auth/ProtectedRoute'

export const router = createBrowserRouter([
  {
    path: '/login',
    element: <LoginPage />,
  },
  {
    element: <ProtectedRoute />,
    children: [
      {
        path: '/',
        element: <FoundationPage />,
      },
    ],
  },
])

