import { Fragment, useState, type FormEvent } from 'react'

import { ActiveBadge } from '@/components/Badges'
import { describeError } from '@/components/error-message'
import { Icon } from '@/components/Icon'
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
  // Error form reset tampil tepat di bawah kolom password, bukan di atas panel.
  const [resetError, setResetError] = useState<string | null>(null)
  // Form tambah user dibuka saat dibutuhkan; daftar user adalah isi utama halaman.
  const [creating, setCreating] = useState(false)
  // Konfirmasi singkat setelah aksi berhasil, agar admin tahu perubahannya tersimpan.
  const [notice, setNotice] = useState<string | null>(null)

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
      setCreating(false)
      setNotice(`User ${username.trim()} ditambahkan dan langsung aktif.`)
    } catch (error) {
      setFormError(describeError(error))
    }
  }

  // updateUser: ganti role atau status aktif. Minta konfirmasi dulu (kecuali mengaktifkan),
  // karena backend akan mencabut session user tersebut.
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

  // openReset: buka/tutup form reset untuk satu user; error form sebelumnya dibuang.
  function openReset(user: AdminUser | null) {
    setResetTarget(user)
    setResetError(null)
  }

  // handleReset: kirim password baru untuk user yang dipilih (minimal 8 karakter).
  async function handleReset(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!resetTarget) {
      return
    }
    const form = event.currentTarget
    const password = String(new FormData(form).get('password') ?? '')
    if (password.length < 8) {
      setResetError('Password baru minimal 8 karakter.')
      form.querySelector<HTMLInputElement>('input[name="password"]')?.focus()
      return
    }
    setResetError(null)
    try {
      await mutations.resetPassword.mutateAsync({ userId: resetTarget.id, password })
      setNotice(`Password ${resetTarget.username} diperbarui. Semua sesinya sudah dicabut.`)
      setResetTarget(null)
    } catch (error) {
      setResetError(describeError(error))
    }
  }

  return (
    <div>
      {creating ? (
        <div className="panel panel--editing">
          <div className="panel__header">
            <div>
              <h2>Tambah user</h2>
              <p className="muted">Password minimal 8 karakter. User baru langsung aktif.</p>
            </div>
          </div>
          <form aria-label="Tambah user" noValidate onSubmit={(event) => void handleCreate(event)}>
            <div className="form-grid">
              <label className="form-field">
                Username
                <input
                  autoCapitalize="none"
                  autoComplete="off"
                  autoFocus
                  name="username"
                  required
                  spellCheck={false}
                />
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
            </div>
            {formError ? (
              <p className="form-error" role="alert">
                <Icon name="error" size={16} />
                {formError}
              </p>
            ) : null}
            <div className="form-actions">
              <button className="button button--primary" disabled={mutations.create.isPending} type="submit">
                <Icon
                  className={mutations.create.isPending ? 'spinner' : undefined}
                  name={mutations.create.isPending ? 'loader' : 'plus'}
                  size={16}
                />
                {mutations.create.isPending ? 'Menyimpan...' : 'Tambah user'}
              </button>
              <button
                className="button button--ghost"
                onClick={() => {
                  setCreating(false)
                  setFormError(null)
                }}
                type="button"
              >
                Batal
              </button>
            </div>
          </form>
        </div>
      ) : null}

      <div className="panel">
        <div className="panel__header">
          <div className="panel__title">
            <h2>Daftar user</h2>
            {users.data ? <span className="badge">{users.data.length}</span> : null}
          </div>
          {!creating ? (
            <button className="button button--primary" onClick={() => setCreating(true)} type="button">
              <Icon name="plus" size={16} />
              Tambah user
            </button>
          ) : null}
        </div>
        {notice ? <StatusMessage tone="success">{notice}</StatusMessage> : null}
        {actionError ? <StatusMessage tone="error">{actionError}</StatusMessage> : null}
        {users.isPending ? <StatusMessage tone="loading">Memuat user...</StatusMessage> : null}
        {users.isError ? (
          <StatusMessage tone="error" title="User gagal dimuat">
            {describeError(users.error)}
          </StatusMessage>
        ) : null}
        {users.data?.length === 0 ? <StatusMessage illustration="searching" tone="empty">Belum ada user.</StatusMessage> : null}

        {users.data && users.data.length > 0 ? (
          <div className="data-table-wrapper">
            <table className="data-table data-table--stack">
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
                  <Fragment key={user.id}>
                  <tr className={resetTarget?.id === user.id ? 'row-selected' : undefined}>
                    <td data-label="Username">
                      <span className="cell-identity">
                        <strong className="cell-truncate" title={user.username}>
                          {user.username}
                        </strong>
                        {user.id === currentUser.data?.id ? <span className="badge badge--brand">Anda</span> : null}
                      </span>
                    </td>
                    <td data-label="Role">
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
                    <td data-label="Status">
                      <ActiveBadge active={user.isActive} />
                    </td>
                    <td className="cell-actions">
                      <div className="inline-actions">
                        <button
                          className={
                            user.isActive
                              ? 'button button--danger-outline button--small'
                              : 'button button--secondary button--small'
                          }
                          onClick={() => void updateUser(user, { isActive: !user.isActive })}
                          type="button"
                        >
                          {user.isActive ? 'Nonaktifkan' : 'Aktifkan'}
                        </button>
                        <button
                          aria-expanded={resetTarget?.id === user.id}
                          className="button button--secondary button--small"
                          onClick={() => openReset(resetTarget?.id === user.id ? null : user)}
                          type="button"
                        >
                          <Icon name="key" size={14} />
                          Reset password
                        </button>
                      </div>
                    </td>
                  </tr>
                  {resetTarget?.id === user.id ? (
                    // Form reset muncul tepat di bawah baris yang dipilih, bukan di kaki panel.
                    <tr className="row-expand">
                      <td colSpan={4}>
                        <form
                          aria-label={`Reset password ${user.username}`}
                          className="row-form"
                          noValidate
                          onSubmit={(event) => void handleReset(event)}
                        >
                          {/* Grid: label / input + tombol / petunjuk / error. Tombol selalu
                              sejajar input walau username sangat panjang (label dipotong "..."). */}
                          <label className="row-form__label" htmlFor={`reset-password-${user.id}`} title={user.username}>
                            Password baru untuk {user.username}
                          </label>
                          <input
                            aria-describedby={`reset-hint-${user.id}${resetError ? ` reset-error-${user.id}` : ''}`}
                            aria-invalid={resetError ? true : undefined}
                            autoComplete="new-password"
                            autoFocus
                            className="input row-form__input"
                            id={`reset-password-${user.id}`}
                            minLength={8}
                            name="password"
                            onChange={() => setResetError(null)}
                            type="password"
                          />
                          <p className="form-field__hint row-form__hint" id={`reset-hint-${user.id}`}>
                            Minimal 8 karakter. Semua sesi user ini akan dicabut.
                          </p>
                          {resetError ? (
                            <p className="form-error row-form__error" id={`reset-error-${user.id}`} role="alert">
                              <Icon name="error" size={16} />
                              {resetError}
                            </p>
                          ) : null}
                          <div className="row-form__actions">
                            <button
                              className="button button--primary"
                              disabled={mutations.resetPassword.isPending}
                              type="submit"
                            >
                              {mutations.resetPassword.isPending ? 'Menyimpan...' : 'Simpan password'}
                            </button>
                            <button className="button button--ghost" onClick={() => openReset(null)} type="button">
                              Batal
                            </button>
                          </div>
                        </form>
                      </td>
                    </tr>
                  ) : null}
                  </Fragment>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>
    </div>
  )
}
