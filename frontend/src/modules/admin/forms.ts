import type { ManagedDatabaseInput, SSLMode } from '@/modules/admin/types'

// Validasi di frontend hanya untuk UX; backend tetap memvalidasi ulang.

export function validateNewUser(username: string, password: string) {
  const trimmed = username.trim()
  if (trimmed.length < 3 || trimmed.length > 64 || /\s/.test(trimmed)) {
    return 'Username harus 3–64 karakter tanpa spasi.'
  }
  if (password.length < 8) {
    return 'Password minimal 8 karakter.'
  }
  return null
}

// readDatabaseForm mengubah isi form menjadi input API. Password kosong pada
// mode edit berarti credential tersimpan tidak diubah.
export function readDatabaseForm(form: HTMLFormElement, requirePassword: boolean) {
  const data = new FormData(form)
  const text = (key: string) => String(data.get(key) ?? '').trim()
  const input: ManagedDatabaseInput = {
    name: text('name'),
    host: text('host'),
    port: Number(text('port') || 5432),
    databaseName: text('databaseName'),
    username: text('username'),
    sslMode: (text('sslMode') || 'require') as SSLMode,
    isActive: data.get('isActive') === 'on',
  }
  const password = String(data.get('password') ?? '')
  if (password !== '') {
    input.password = password
  }

  let error: string | null = null
  if (!input.name || !input.host || !input.databaseName || !input.username) {
    error = 'Nama, host, nama database, dan username wajib diisi.'
  } else if (!Number.isInteger(input.port) || input.port < 1 || input.port > 65535) {
    error = 'Port harus antara 1 dan 65535.'
  } else if (requirePassword && !input.password) {
    error = 'Password wajib diisi untuk koneksi baru.'
  }
  return { input, error }
}
