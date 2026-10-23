import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Route } from 'react-router-dom'

import { RequirePlatformAdmin } from '@/modules/admin/RequirePlatformAdmin'
import { adminUser, regularUser, renderRoute } from '@/test/platform-test-utils'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderGuard(user = regularUser) {
  renderRoute(<></>, {
    route: '/admin',
    path: '/unused',
    user,
    extraRoutes: (
      <Route element={<RequirePlatformAdmin />}>
        <Route path="/admin" element={<h1>Panel admin</h1>} />
      </Route>
    ),
  })
}

describe('RequirePlatformAdmin', () => {
  it('menampilkan forbidden state untuk user biasa', () => {
    renderGuard(regularUser)

    expect(screen.getByRole('alert')).toHaveTextContent('Akses ditolak')
    expect(screen.queryByRole('heading', { name: 'Panel admin' })).not.toBeInTheDocument()
  })

  it('menampilkan halaman admin untuk platform admin', () => {
    renderGuard(adminUser)

    expect(screen.getByRole('heading', { name: 'Panel admin' })).toBeInTheDocument()
  })
})
