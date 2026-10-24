import { useState } from 'react'

import type { DraftInput } from '@/modules/migrations/types'

const MAX_SQL_BYTES = 256 * 1024

interface MigrationEditorProps {
  initialValue?: DraftInput
  disabled?: boolean
  pending?: boolean
  submitLabel: string
  onSubmit: (input: DraftInput) => void
  transactionalDdl?: boolean
}

export function MigrationEditor({
  initialValue = { name: '', sql: '' },
  disabled = false,
  pending = false,
  submitLabel,
  onSubmit,
  transactionalDdl = true,
}: MigrationEditorProps) {
  const [name, setName] = useState(initialValue.name)
  const [sql, setSQL] = useState(initialValue.sql)
  const [validation, setValidation] = useState<string | null>(null)

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const byteLength = new TextEncoder().encode(sql).byteLength
    if (!name.trim() || !sql.trim()) {
      setValidation('Nama dan SQL migration wajib diisi.')
      return
    }
    if (name.trim().length > 160 || byteLength > MAX_SQL_BYTES) {
      setValidation('Nama maksimal 160 karakter dan SQL maksimal 256 KiB.')
      return
    }
    setValidation(null)
    onSubmit({ name: name.trim(), sql })
  }

  return (
    <form className="migration-editor" onSubmit={submit}>
      <label>
        Nama migration
        <input disabled={disabled || pending} maxLength={160} onChange={(event) => setName(event.target.value)} value={name} />
      </label>
      <label>
        SQL
        <textarea aria-describedby="sql-help" disabled={disabled || pending} onChange={(event) => setSQL(event.target.value)} rows={18} spellCheck={false} value={sql} />
      </label>
      <p className="migration-help" id="sql-help">
        {!transactionalDdl
          ? 'Target ini hanya menerima statement DML transaksional (SELECT, INSERT, UPDATE, DELETE). DDL ditolak karena dapat melakukan implicit commit.'
          : 'Seluruh SQL dijalankan dalam satu transaction. BEGIN, COMMIT, ROLLBACK, dan kontrol transaction lain tidak diperbolehkan.'}
      </p>
      {validation ? <p className="migration-alert" role="alert">{validation}</p> : null}
      {!disabled ? (
        <button className="button button--primary" disabled={pending} type="submit">
          {pending ? 'Menyimpan…' : submitLabel}
        </button>
      ) : null}
    </form>
  )
}
