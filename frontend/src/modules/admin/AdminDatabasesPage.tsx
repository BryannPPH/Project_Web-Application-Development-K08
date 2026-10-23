import { useState, type FormEvent } from 'react'

import { describeError } from '@/components/error-message'
import { StatusMessage } from '@/components/StatusMessage'
import { useManagedDatabaseMutations, useManagedDatabases } from '@/modules/admin/api'
import { readDatabaseForm } from '@/modules/admin/forms'
import { sslModes, type ConnectionTestResult, type ManagedDatabase } from '@/modules/admin/types'

const reasonLabels: Record<string, string> = {
  timeout: 'koneksi timeout',
  authentication_failed: 'username/password ditolak',
  database_not_found: 'nama database tidak ditemukan',
  network_unreachable: 'host/port tidak dapat dihubungi',
  tls_failed: 'negosiasi TLS/SSL gagal',
  too_many_connections: 'server menolak koneksi baru',
  invalid_configuration: 'konfigurasi tidak valid',
  credential_unavailable: 'credential tersimpan tidak dapat dibuka',
  unknown: 'penyebab tidak diketahui',
}

function describeTest(result: ConnectionTestResult) {
  if (result.success) {
    return `Koneksi berhasil (${result.latencyMs} ms).`
  }
  return `Koneksi gagal: ${reasonLabels[result.reason ?? 'unknown'] ?? result.reason}.`
}

interface DatabaseFormProps {
  initial?: ManagedDatabase
  onDone: () => void
}

function DatabaseForm({ initial, onDone }: DatabaseFormProps) {
  const mutations = useManagedDatabaseMutations()
  const [message, setMessage] = useState<{ tone: 'info' | 'error'; text: string } | null>(null)
  const editing = !!initial

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const { input, error } = readDatabaseForm(event.currentTarget, !editing)
    if (error) {
      setMessage({ tone: 'error', text: error })
      return
    }
    try {
      if (initial) {
        await mutations.update.mutateAsync({ databaseId: initial.id, input })
      } else {
        await mutations.create.mutateAsync(input)
      }
      onDone()
    } catch (submitError) {
      setMessage({ tone: 'error', text: describeError(submitError) })
    }
  }

  async function handleTest(form: HTMLFormElement | null) {
    if (!form) {
      return
    }
    const { input, error } = readDatabaseForm(form, true)
    if (error) {
      setMessage({ tone: 'error', text: `${error} (Test koneksi butuh password.)` })
      return
    }
    try {
      const result = await mutations.testDraft.mutateAsync(input)
      setMessage({ tone: result.success ? 'info' : 'error', text: describeTest(result) })
    } catch (testError) {
      setMessage({ tone: 'error', text: describeError(testError) })
    }
  }

  const pending = mutations.create.isPending || mutations.update.isPending

  return (
    <form
      aria-label={editing ? 'Ubah koneksi database' : 'Tambah koneksi database'}
      noValidate
      onSubmit={(event) => void handleSubmit(event)}
    >
      <div className="form-grid">
        <label className="form-field">
          Nama koneksi
          <input defaultValue={initial?.name} name="name" />
        </label>
        <label className="form-field">
          Host
          <input defaultValue={initial?.host} name="host" placeholder="localhost" />
        </label>
        <label className="form-field">
          Port
          <input defaultValue={initial?.port ?? 5432} inputMode="numeric" name="port" />
        </label>
        <label className="form-field">
          Nama database
          <input defaultValue={initial?.databaseName} name="databaseName" />
        </label>
        <label className="form-field">
          Username
          <input autoComplete="off" defaultValue={initial?.username} name="username" />
        </label>
        <label className="form-field">
          Password
          {/* Password tidak pernah diprefill: backend hanya mengembalikan hasCredential. */}
          <input
            autoComplete="new-password"
            name="password"
            placeholder={editing ? 'Kosongkan bila tidak diubah' : ''}
            type="password"
          />
        </label>
        <label className="form-field">
          SSL mode
          <select defaultValue={initial?.sslMode ?? 'require'} name="sslMode">
            {sslModes.map((mode) => (
              <option key={mode} value={mode}>
                {mode}
              </option>
            ))}
          </select>
        </label>
        <label className="form-field form-field--checkbox">
          <input defaultChecked={initial?.isActive ?? true} name="isActive" type="checkbox" />
          Aktif
        </label>
      </div>
      {message ? <StatusMessage tone={message.tone}>{message.text}</StatusMessage> : null}
      <div className="form-actions">
        <button className="button button--primary" disabled={pending} type="submit">
          {pending ? 'Menyimpan…' : editing ? 'Simpan perubahan' : 'Tambah koneksi'}
        </button>
        <button
          className="button button--secondary"
          disabled={mutations.testDraft.isPending}
          onClick={(event) => void handleTest(event.currentTarget.form)}
          type="button"
        >
          {mutations.testDraft.isPending ? 'Menguji…' : 'Test koneksi'}
        </button>
        {editing ? (
          <button className="button button--secondary" onClick={onDone} type="button">
            Batal
          </button>
        ) : null}
      </div>
    </form>
  )
}

export function AdminDatabasesPage() {
  const databases = useManagedDatabases()
  const mutations = useManagedDatabaseMutations()
  const [editing, setEditing] = useState<ManagedDatabase | null>(null)
  const [formKey, setFormKey] = useState(0)
  const [rowMessage, setRowMessage] = useState<{ tone: 'info' | 'error'; text: string } | null>(null)

  async function testStored(database: ManagedDatabase) {
    try {
      const result = await mutations.testStored.mutateAsync(database.id)
      setRowMessage({ tone: result.success ? 'info' : 'error', text: `${database.name}: ${describeTest(result)}` })
    } catch (error) {
      setRowMessage({ tone: 'error', text: describeError(error) })
    }
  }

  async function remove(database: ManagedDatabase) {
    if (!window.confirm(`Hapus koneksi ${database.name}? Grant group untuk database ini ikut terhapus.`)) {
      return
    }
    try {
      await mutations.remove.mutateAsync(database.id)
      setRowMessage({ tone: 'info', text: `${database.name} dihapus.` })
    } catch (error) {
      setRowMessage({ tone: 'error', text: describeError(error) })
    }
  }

  return (
    <div>
      <div className="panel">
        <h2>{editing ? `Ubah ${editing.name}` : 'Tambah koneksi PostgreSQL'}</h2>
        <DatabaseForm
          initial={editing ?? undefined}
          key={editing?.id ?? `new-${formKey}`}
          onDone={() => {
            setEditing(null)
            setFormKey((value) => value + 1)
          }}
        />
      </div>

      <div className="panel">
        <h2>Koneksi terdaftar</h2>
        {rowMessage ? <StatusMessage tone={rowMessage.tone}>{rowMessage.text}</StatusMessage> : null}
        {databases.isPending ? <StatusMessage>Memuat koneksi…</StatusMessage> : null}
        {databases.isError ? (
          <StatusMessage tone="error" title="Koneksi gagal dimuat">
            {describeError(databases.error)}
          </StatusMessage>
        ) : null}
        {databases.data?.length === 0 ? (
          <StatusMessage tone="empty">Belum ada koneksi database.</StatusMessage>
        ) : null}
        {databases.data && databases.data.length > 0 ? (
          <div className="data-table-wrapper">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Nama</th>
                  <th>Target</th>
                  <th>SSL</th>
                  <th>Credential</th>
                  <th>Status</th>
                  <th>Aksi</th>
                </tr>
              </thead>
              <tbody>
                {databases.data.map((database) => (
                  <tr key={database.id}>
                    <td>{database.name}</td>
                    <td>
                      <code>
                        {database.username}@{database.host}:{database.port}/{database.databaseName}
                      </code>
                    </td>
                    <td>{database.sslMode}</td>
                    <td>{database.hasCredential ? 'Tersimpan (terenkripsi)' : 'Belum ada'}</td>
                    <td>
                      <span className={database.isActive ? 'badge badge--success' : 'badge badge--danger'}>
                        {database.isActive ? 'Aktif' : 'Nonaktif'}
                      </span>
                    </td>
                    <td>
                      <div className="inline-actions">
                        <button className="button button--secondary button--small" onClick={() => void testStored(database)} type="button">
                          Test
                        </button>
                        <button className="button button--secondary button--small" onClick={() => setEditing(database)} type="button">
                          Ubah
                        </button>
                        <button className="button button--danger button--small" onClick={() => void remove(database)} type="button">
                          Hapus
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>
    </div>
  )
}
