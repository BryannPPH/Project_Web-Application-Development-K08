import { ApiError } from '@/lib/api-client'

// Pesan Bahasa Indonesia untuk kode error backend yang sering muncul.
const messages: Record<string, string> = {
  FORBIDDEN: 'Anda tidak memiliki izin untuk aksi ini.',
  DATABASE_NOT_FOUND: 'Database tidak ditemukan atau Anda tidak memiliki akses.',
  DATABASE_INACTIVE: 'Database sedang dinonaktifkan oleh administrator.',
  DATABASE_EDIT_FORBIDDEN: 'Aksi ini membutuhkan akses View + Edit.',
  TARGET_UNAVAILABLE: 'Database target tidak dapat dihubungi. Coba lagi nanti.',
  TARGET_TIMEOUT: 'Query ke database target melewati batas waktu.',
  TARGET_PERMISSION_DENIED: 'Role koneksi platform tidak memiliki izin membaca objek ini.',
  TARGET_QUERY_FAILED: 'Query ke database target gagal.',
  SCHEMA_NOT_FOUND: 'Schema tidak ditemukan.',
  TABLE_NOT_FOUND: 'Table tidak ditemukan.',
  INVALID_PAGINATION: 'Parameter halaman tidak valid.',
  USERNAME_TAKEN: 'Username sudah digunakan.',
  GROUP_NAME_TAKEN: 'Nama group sudah digunakan.',
  DATABASE_NAME_TAKEN: 'Nama koneksi database sudah digunakan.',
  DATABASE_HAS_MIGRATIONS:
    'Database memiliki riwayat migration sehingga tidak dapat dihapus. Nonaktifkan saja.',
  LAST_ACTIVE_ADMIN:
    'Administrator aktif terakhir tidak dapat diturunkan atau dinonaktifkan.',
  INVALID_CSRF: 'Token keamanan tidak valid. Muat ulang halaman lalu coba lagi.',
}

export function describeError(error: unknown) {
  if (error instanceof ApiError) {
    return messages[error.code] ?? error.message
  }
  return 'Terjadi kesalahan. Silakan coba lagi.'
}
