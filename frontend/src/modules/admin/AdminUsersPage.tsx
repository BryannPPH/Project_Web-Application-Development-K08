import { useState, type FormEvent } from 'react'

import { describeError } from '@/components/error-message'
import { StatusMessage } from '@/components/StatusMessage'
import { useAdminUsers, useUserMutations } from '@/modules/admin/api'
import { validateNewUser } from '@/modules/admin/forms'
import type { AdminUser } from '@/modules/admin/types'
import type { PlatformRole } from '@/modules/auth/types'
import { useCurrentUser } from '@/modules/auth/use-current-user'

const roleLabels: Record<PlatformRole, string> = {
  user: 'User',
  platform_admin: 'Platform admin',
}

export function AdminUsersPage() {
  const users = useAdminUsers()
  const mutations = useUserMutations()
  const currentUser = useCurrentUser()
  const [formError, setFormError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [resetTarget, setResetTarget] = useState<AdminUser | null>(null)

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    const username = String(data.get('username') ?? '')
    const password = String(data.get('password') ?? '')
    const platformRole = data.get('platformRole') === 'platform_admin' ? 'platform_admin' : 'user'

    const validation = validateNewUser(username, password)
    setFormError(validation)
    if (validation) {
      return
    }
    try {
      await mutations.create.mutateAsync({ username: username.trim(), password, platformRole })
      form.reset()
    } catch (error) {
      setFormError(describeError(error))
    }
  }

  async function updateUser(user: AdminUser, input: { platformRole?: PlatformRole; isActive?: boolean }) {
    const consequence =
      input.isActive === false
        ? `Nonaktifkan ${user.username}? Seluruh session user akan dicabut.`
        : `Ubah role ${user.username}? Seluruh session user akan dicabut dan user harus login ulang.`
    if (input.isActive !== true && !window.confirm(consequence)) {
      return
    }
    setActionError(null)
    try {
      await mutations.update.mutateAsync({ userId: user.id, input })
    } catch (error) {
      setActionError(describeError(error))
    }
  }

  async function handleReset(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!resetTarget) {
      return
    }
    const form = event.currentTarget
    const password = String(new FormData(form).get('password') ?? '')
    if (password.length < 8) {
      setActionError('Password baru minimal 8 karakter.')
      return
    }
    try {
      await mutations.resetPassword.mutateAsync({ userId: resetTarget.id, password })
      setResetTarget(null)
      setActionError(null)
    } catch (error) {
      setActionError(describeError(error))
    }
  }

  return (
    <div>
      <div className="panel">
        <h2>Tambah user</h2>
        <form aria-label="Tambah user" noValidate onSubmit={(event) => void handleCreate(event)}>
          <div className="form-grid">
            <label className="form-field">
              Username
              <input autoComplete="off" name="username" required />
            </label>
            <label className="form-field">
              Password awal
              <input autoComplete="new-password" name="password" required type="password" />
            </label>
            <label className="form-field">
              Role platform
              <select defaultValue="user" name="platformRole">
                <option value="user">User</option>
                <option value="platform_admin">Platform admin</option>
              </select>
            </label>
            <button className="button button--primary" disabled={mutations.create.isPending} type="submit">
              {mutations.create.isPending ? 'Menyimpan…' : 'Tambah'}
            </button>
          </div>
          {formError ? (
            <p className="form-error" role="alert">
              {formError}
            </p>
          ) : null}
        </form>
      </div>

      <div className="panel">
        <h2>Daftar user</h2>
        {actionError ? <StatusMessage tone="error">{actionError}</StatusMessage> : null}
        {users.isPending ? <StatusMessage>Memuat user…</StatusMessage> : null}
        {users.isError ? (
          <StatusMessage tone="error" title="User gagal dimuat">
            {describeError(users.error)}
          </StatusMessage>
        ) : null}
        {users.data?.length === 0 ? <StatusMessage tone="empty">Belum ada user.</StatusMessage> : null}

        {users.data && users.data.length > 0 ? (
          <div className="data-table-wrapper">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Username</th>
                  <th>Role</th>
                  <th>Status</th>
                  <th>Aksi</th>
                </tr>
              </thead>
              <tbody>
                {users.data.map((user) => (
                  <tr key={user.id}>
                    <td>
                      {user.username}
                      {user.id === currentUser.data?.id ? <span className="muted"> (Anda)</span> : null}
                    </td>
                    <td>
                      <select
                        aria-label={`Role ${user.username}`}
                        onChange={(event) =>
                          void updateUser(user, { platformRole: event.target.value as PlatformRole })
                        }
                        value={user.platformRole}
                      >
                        {Object.entries(roleLabels).map(([value, label]) => (
                          <option key={value} value={value}>
                            {label}
                          </option>
                        ))}
                      </select>
                    </td>
                    <td>
                      <span className={user.isActive ? 'badge badge--success' : 'badge badge--danger'}>
                        {user.isActive ? 'Aktif' : 'Nonaktif'}
                      </span>
                    </td>
                    <td>
                      <div className="inline-actions">
                        <button
                          className="button button--secondary button--small"
                          onClick={() => void updateUser(user, { isActive: !user.isActive })}
                          type="button"
                        >
                          {user.isActive ? 'Nonaktifkan' : 'Aktifkan'}
                        </button>
                        <button
                          className="button button--secondary button--small"
                          onClick={() => setResetTarget(user)}
                          type="button"
                        >
                          Reset password
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}

        {resetTarget ? (
          <form
            aria-label="Reset password"
            className="panel"
            noValidate
            onSubmit={(event) => void handleReset(event)}
          >
            <h3>Reset password {resetTarget.username}</h3>
            <p className="muted">Seluruh session user ini akan dicabut setelah password diganti.</p>
            <div className="form-grid">
              <label className="form-field">
                Password baru
                <input autoComplete="new-password" name="password" type="password" />
              </label>
            </div>
            <div className="form-actions">
              <button className="button button--primary" disabled={mutations.resetPassword.isPending} type="submit">
                Simpan password
              </button>
              <button className="button button--secondary" onClick={() => setResetTarget(null)} type="button">
                Batal
              </button>
            </div>
          </form>
        ) : null}
      </div>
    </div>
  )
}
