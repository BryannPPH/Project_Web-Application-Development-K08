import { createBrowserRouter, Navigate } from 'react-router-dom'

import { DashboardPage } from '@/app/DashboardPage'
import { FoundationPage } from '@/app/FoundationPage'
import { AdminDatabasesPage } from '@/modules/admin/AdminDatabasesPage'
import { AdminGroupsPage } from '@/modules/admin/AdminGroupsPage'
import { AdminLayout } from '@/modules/admin/AdminLayout'
import { AdminUsersPage } from '@/modules/admin/AdminUsersPage'
import { RequirePlatformAdmin } from '@/modules/admin/RequirePlatformAdmin'
import { LoginPage } from '@/modules/auth/LoginPage'
import { ProtectedRoute } from '@/modules/auth/ProtectedRoute'
import { DatabaseListPage } from '@/modules/databases/DatabaseListPage'
import { ViewerPage } from '@/modules/viewer/ViewerPage'

export const router = createBrowserRouter([
  {
    path: '/login',
    element: <LoginPage />,
  },
  {
    element: <ProtectedRoute />,
    children: [
      {
        element: <FoundationPage />,
        children: [
          { path: '/', element: <DashboardPage /> },
          { path: '/databases', element: <DatabaseListPage /> },
          { path: '/databases/:databaseId', element: <ViewerPage /> },
          { path: '/databases/:databaseId/schemas/:schema', element: <ViewerPage /> },
          { path: '/databases/:databaseId/schemas/:schema/tables/:table', element: <ViewerPage /> },
          {
            element: <RequirePlatformAdmin />,
            children: [
              {
                path: '/admin',
                element: <AdminLayout />,
                children: [
                  { index: true, element: <Navigate replace to="/admin/users" /> },
                  { path: 'users', element: <AdminUsersPage /> },
                  { path: 'groups', element: <AdminGroupsPage /> },
                  { path: 'databases', element: <AdminDatabasesPage /> },
                ],
              },
            ],
          },
        ],
      },
    ],
  },
])
