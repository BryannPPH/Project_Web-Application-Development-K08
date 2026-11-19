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
