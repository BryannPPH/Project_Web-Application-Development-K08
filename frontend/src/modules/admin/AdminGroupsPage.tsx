import { useState, type FormEvent } from 'react'

import { describeError } from '@/components/error-message'
import { StatusMessage } from '@/components/StatusMessage'
import {
  useAdminGroups,
  useAdminUsers,
  useGroupGrants,
  useGroupMembers,
  useGroupMutations,
  useManagedDatabases,
} from '@/modules/admin/api'
import type { AdminGroup } from '@/modules/admin/types'
import { accessLevelLabel, type AccessLevel } from '@/modules/databases/types'

export function AdminGroupsPage() {
  const groups = useAdminGroups()
  const mutations = useGroupMutations()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const selected = groups.data?.find((group) => group.id === selectedId) ?? null

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    const name = String(data.get('name') ?? '').trim()
    const description = String(data.get('description') ?? '').trim()
    if (!name) {
      setError('Nama group wajib diisi.')
      return
    }
    try {
      const created = await mutations.create.mutateAsync({ name, description: description || undefined })
      form.reset()
      setError(null)
      setSelectedId(created.group.id)
    } catch (createError) {
      setError(describeError(createError))
    }
  }

  return (
    <div>
      <div className="panel">
        <h2>Tambah group</h2>
        <form aria-label="Tambah group" noValidate onSubmit={(event) => void handleCreate(event)}>
          <div className="form-grid">
            <label className="form-field">
              Nama group
              <input name="name" />
            </label>
            <label className="form-field">
              Deskripsi
              <input name="description" />
            </label>
            <button className="button button--primary" disabled={mutations.create.isPending} type="submit">
              Tambah
            </button>
          </div>
          {error ? (
            <p className="form-error" role="alert">
              {error}
            </p>
          ) : null}
        </form>
      </div>

      <div className="panel">
        <h2>Daftar group</h2>
        {groups.isPending ? <StatusMessage>Memuat group…</StatusMessage> : null}
        {groups.isError ? <StatusMessage tone="error">{describeError(groups.error)}</StatusMessage> : null}
        {groups.data?.length === 0 ? <StatusMessage tone="empty">Belum ada group.</StatusMessage> : null}
        {groups.data && groups.data.length > 0 ? (
          <div className="data-table-wrapper">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Nama</th>
                  <th>Deskripsi</th>
                  <th>Anggota</th>
                  <th aria-label="Aksi" />
                </tr>
              </thead>
              <tbody>
                {groups.data.map((group) => (
                  <tr key={group.id}>
                    <td>{group.name}</td>
                    <td>{group.description ?? <span className="cell-null">—</span>}</td>
                    <td>{group.memberCount}</td>
                    <td>
                      <button
                        className="button button--secondary button--small"
                        onClick={() => setSelectedId(group.id)}
                        type="button"
                      >
                        Kelola
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>

      {selected ? (
        <GroupDetail group={selected} key={selected.id} onDeleted={() => setSelectedId(null)} />
      ) : null}
    </div>
  )
}

function GroupDetail({ group, onDeleted }: { group: AdminGroup; onDeleted: () => void }) {
  const mutations = useGroupMutations()
  const members = useGroupMembers(group.id)
  const grants = useGroupGrants(group.id)
  const users = useAdminUsers()
  const databases = useManagedDatabases()
  const [message, setMessage] = useState<{ tone: 'info' | 'error'; text: string } | null>(null)

  async function run(action: () => Promise<unknown>, success: string) {
    try {
      await action()
      setMessage({ tone: 'info', text: success })
    } catch (error) {
      setMessage({ tone: 'error', text: describeError(error) })
    }
  }

  function handleRename(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const name = String(data.get('name') ?? '').trim()
    const description = String(data.get('description') ?? '').trim()
    if (!name) {
      setMessage({ tone: 'error', text: 'Nama group wajib diisi.' })
      return
    }
    void run(
      () => mutations.update.mutateAsync({ groupId: group.id, input: { name, description } }),
      'Group diperbarui.',
    )
  }

  function handleDelete() {
    if (!window.confirm(`Hapus group ${group.name}? Seluruh membership dan grant group ini ikut terhapus.`)) {
      return
    }
    void run(async () => {
      await mutations.remove.mutateAsync(group.id)
      onDeleted()
    }, 'Group dihapus.')
  }

  function handleAddMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const userId = String(new FormData(event.currentTarget).get('userId') ?? '')
    if (!userId) {
      return
    }
    void run(() => mutations.addMember.mutateAsync({ groupId: group.id, userId }), 'Anggota ditambahkan.')
  }

  function handleGrant(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const databaseId = String(data.get('databaseId') ?? '')
    const accessLevel = data.get('accessLevel') === 'view_edit' ? 'view_edit' : 'view_only'
    if (!databaseId) {
      return
    }
    void run(
      () => mutations.upsertGrant.mutateAsync({ groupId: group.id, databaseId, accessLevel }),
      'Akses database disimpan.',
    )
  }

  const memberIds = new Set(members.data?.map((member) => member.userId))
  const candidates = users.data?.filter((user) => !memberIds.has(user.id)) ?? []

  return (
    <div className="panel">
      <h2>Kelola group: {group.name}</h2>
      {message ? <StatusMessage tone={message.tone}>{message.text}</StatusMessage> : null}

      <form aria-label="Ubah group" noValidate onSubmit={handleRename}>
        <div className="form-grid">
          <label className="form-field">
            Nama
            <input defaultValue={group.name} name="name" />
          </label>
          <label className="form-field">
            Deskripsi
            <input defaultValue={group.description ?? ''} name="description" />
          </label>
          <div className="inline-actions">
            <button className="button button--primary" type="submit">
              Simpan
            </button>
            <button className="button button--danger" onClick={handleDelete} type="button">
              Hapus group
            </button>
          </div>
        </div>
      </form>

      <h3>Anggota</h3>
      {members.isPending ? <StatusMessage>Memuat anggota…</StatusMessage> : null}
      {members.isError ? <StatusMessage tone="error">{describeError(members.error)}</StatusMessage> : null}
      {members.data?.length === 0 ? <StatusMessage tone="empty">Group belum memiliki anggota.</StatusMessage> : null}
      {members.data && members.data.length > 0 ? (
        <div className="data-table-wrapper">
          <table className="data-table">
            <tbody>
              {members.data.map((member) => (
                <tr key={member.userId}>
                  <td>{member.username}</td>
                  <td>{member.isActive ? null : <span className="badge badge--danger">Nonaktif</span>}</td>
                  <td>
                    <button
                      className="button button--secondary button--small"
                      onClick={() =>
                        void run(
                          () => mutations.removeMember.mutateAsync({ groupId: group.id, userId: member.userId }),
                          'Anggota dikeluarkan.',
                        )
                      }
                      type="button"
                    >
                      Keluarkan
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      <form aria-label="Tambah anggota" className="form-actions" onSubmit={handleAddMember}>
        <label className="form-field">
          Tambah anggota
          <select defaultValue="" name="userId">
            <option disabled value="">
              Pilih user
            </option>
            {candidates.map((user) => (
              <option key={user.id} value={user.id}>
                {user.username}
              </option>
            ))}
          </select>
        </label>
        <button className="button button--secondary" type="submit">
          Tambah
        </button>
      </form>

      <h3>Akses database</h3>
      <p className="muted">
        Effective access user = level tertinggi dari seluruh group yang diikutinya (View + Edit &gt; View Only).
      </p>
      {grants.isPending ? <StatusMessage>Memuat akses…</StatusMessage> : null}
      {grants.isError ? <StatusMessage tone="error">{describeError(grants.error)}</StatusMessage> : null}
      {grants.data?.length === 0 ? <StatusMessage tone="empty">Group belum memiliki akses database.</StatusMessage> : null}
      {grants.data && grants.data.length > 0 ? (
        <div className="data-table-wrapper">
          <table className="data-table">
            <tbody>
              {grants.data.map((grant) => (
                <tr key={grant.databaseId}>
                  <td>{grant.databaseName}</td>
                  <td>
                    <span className="badge">{accessLevelLabel(grant.accessLevel)}</span>
                  </td>
                  <td>
                    <button
                      className="button button--secondary button--small"
                      onClick={() =>
                        void run(
                          () => mutations.revokeGrant.mutateAsync({ groupId: group.id, databaseId: grant.databaseId }),
                          'Akses dicabut.',
                        )
                      }
                      type="button"
                    >
                      Cabut
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      <form aria-label="Atur akses database" className="form-actions" onSubmit={handleGrant}>
        <label className="form-field">
          Database
          <select defaultValue="" name="databaseId">
            <option disabled value="">
              Pilih database
            </option>
            {databases.data?.map((database) => (
              <option key={database.id} value={database.id}>
                {database.name}
              </option>
            ))}
          </select>
        </label>
        <label className="form-field">
          Tingkat akses
          <select defaultValue={'view_only' satisfies AccessLevel} name="accessLevel">
            <option value="view_only">View Only</option>
            <option value="view_edit">View + Edit</option>
          </select>
        </label>
        <button className="button button--secondary" type="submit">
          Simpan akses
        </button>
      </form>
    </div>
  )
}
