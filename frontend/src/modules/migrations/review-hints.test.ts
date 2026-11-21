import { describe, expect, it } from 'vitest'

import { compatibilityHints } from '@/modules/migrations/review-hints'

const titles = (sql: string) => compatibilityHints(sql).map((hint) => hint.title)

describe('compatibilityHints', () => {
  it('tidak memberi peringatan untuk perubahan aditif yang aman', () => {
    expect(titles('ALTER TABLE customers ADD COLUMN points int;')).toEqual([])
    expect(titles('CREATE INDEX customers_email_idx ON customers (email);')).toEqual([])
  })

  it('mendeteksi perubahan yang merusak', () => {
    expect(titles('ALTER TABLE customers DROP COLUMN email;')).toContain('Menghapus kolom')
    expect(titles('DROP TABLE legacy_orders;')).toContain('Menghapus tabel')
    expect(titles('ALTER TABLE customers RENAME COLUMN email TO mail;')).toContain('Mengganti nama tabel/kolom')
    expect(titles('ALTER TABLE customers ALTER COLUMN points TYPE bigint;')).toContain('Mengubah tipe kolom')
  })

  it('mendeteksi NOT NULL tanpa DEFAULT, tapi tidak jika ada DEFAULT', () => {
    expect(titles('ALTER TABLE customers ADD COLUMN tier text NOT NULL;')).toContain('Kolom NOT NULL tanpa DEFAULT')
    expect(titles("ALTER TABLE customers ADD COLUMN tier text NOT NULL DEFAULT 'basic';")).not.toContain('Kolom NOT NULL tanpa DEFAULT')
  })

  it('mendeteksi UPDATE/DELETE tanpa WHERE', () => {
    expect(titles('DELETE FROM sessions;')).toContain('UPDATE/DELETE tanpa WHERE')
    expect(titles('DELETE FROM sessions WHERE expired;')).not.toContain('UPDATE/DELETE tanpa WHERE')
  })

  it('mengabaikan pola di dalam komentar', () => {
    expect(titles('-- DROP TABLE customers\nALTER TABLE customers ADD COLUMN points int;')).toEqual([])
  })
})
