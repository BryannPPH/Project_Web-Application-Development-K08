/*
 * FILE INI UNTUK APA (schema-diff/api.ts) - BONUS 2: pemanggil API Schema Diff
 * GUIDEBOOK: §3.3 (bonus) + bonus_explanation.md "Bonus 2"
 *   compareSchemas -> POST /api/v1/schema-diff {source, target} -> SchemaDiffResult
 *                     (apiRequest Person 1 otomatis menambah header CSRF)
 *   buildDiffSQL   -> menyusun ulang teks SQL di BROWSER dari perubahan yang
 *                     dicentang user (tanpa request ulang ke backend). Formatnya sama
 *                     dengan backend: "-- <deskripsi>" lalu SQL-nya.
 *                     Urutan tetap benar karena daftar changes dari backend sudah
 *                     terurut per fase (hapus FK dulu ... tambah FK terakhir).
 */
import { apiRequest } from '@/lib/api-client'
import type { SchemaChange, SchemaDiffResult, SchemaRef } from '@/modules/schema-diff/types'

// Membandingkan schema "desired" (source) dengan schema "current" (target).
export function compareSchemas(source: SchemaRef, target: SchemaRef) {
  return apiRequest<SchemaDiffResult>('/schema-diff', { method: 'POST', json: { source, target } })
}

// Menyusun SQL dari perubahan terpilih dengan urutan yang sama seperti backend.
export function buildDiffSQL(changes: SchemaChange[]) {
  return changes.map((change) => `-- ${change.description}\n${change.sql.join('\n')}\n`).join('\n')
}
