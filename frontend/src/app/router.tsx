import { createBrowserRouter } from 'react-router-dom'

import { FoundationPage } from '@/app/FoundationPage'
import { LoginPage } from '@/modules/auth/LoginPage'
import { ProtectedRoute } from '@/modules/auth/ProtectedRoute'
import { MigrationDetailPage } from '@/modules/migrations/MigrationDetailPage'
import { MigrationListPage } from '@/modules/migrations/MigrationListPage'

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
      {
        path: '/databases/:databaseId/migrations',
        element: <MigrationListPage />,
      },
      {
        path: '/databases/:databaseId/migrations/new',
        element: <MigrationDetailPage create />,
      },
      {
        path: '/databases/:databaseId/migrations/:migrationId',
        element: <MigrationDetailPage />,
      },
    ],
  },
])

