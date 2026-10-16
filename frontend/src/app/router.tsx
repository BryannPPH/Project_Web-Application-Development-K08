import { createBrowserRouter } from 'react-router-dom'

import { FoundationPage } from '@/app/FoundationPage'

export const router = createBrowserRouter([
  {
    path: '/',
    element: <FoundationPage />,
  },
])

