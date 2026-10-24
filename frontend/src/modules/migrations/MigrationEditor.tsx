import { useId, useState } from 'react'

import { Icon } from '@/components/Icon'
import type { DraftInput } from '@/modules/migrations/types'

const MAX_SQL_BYTES = 256 * 1024

interface MigrationEditorProps {
  initialValue?: DraftInput
  disabled?: boolean
  pending?: boolean
  submitLabel: string
  onSubmit: (input: DraftInput) => void
  // false untuk target (mis. MySQL) yang melakukan implicit commit pada DDL.
  transactionalDdl?: boolean
  // Dipanggil setiap SQL berubah, mis. agar panel EXPLAIN memakai SQL terbaru.
  onSqlChange?: (sql: string) => void
}

export function MigrationEditor({
  initialValue = { name: '', sql: '' },
  disabled = false,
  pending = false,
  submitLabel,
  onSubmit,
  transactionalDdl = true,
  onSqlChange,
}: MigrationEditorProps) {
  const [name, setName] = useState(initialValue.name)
  const [sql, setSQL] = useState(initialValue.sql)
  const [validation, setValidation] = useState<string | null>(null)
  const sqlId = useId()
  const lineCount = Math.max(sql.split('\n').length, 12)

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
    <form className="panel migration-editor" onSubmit={submit}>
      <label className="form-field">
        Nama migration
        <input
          disabled={disabled || pending}
          maxLength={160}
          onChange={(event) => setName(event.target.value)}
          placeholder="contoh: add_loyalty_points"
          value={name}
        />
      </label>
      <div className="form-field">
        <label htmlFor={sqlId}>SQL</label>
        <div className="sql-editor">
          <div aria-hidden="true" className="sql-editor__gutter">
            {Array.from({ length: lineCount }, (_, index) => (
              <span key={index}>{index + 1}</span>
            ))}
          </div>
          <textarea
            aria-describedby="sql-help"
            disabled={disabled || pending}
            id={sqlId}
            onChange={(event) => {
              setSQL(event.target.value)
              onSqlChange?.(event.target.value)
            }}
            rows={lineCount}
            spellCheck={false}
            value={sql}
          />
        </div>
      </div>
      <p className="migration-help" id="sql-help">
        <Icon name="info" size={14} />
        {transactionalDdl
          ? 'Seluruh SQL dijalankan dalam satu transaction. BEGIN, COMMIT, ROLLBACK, dan kontrol transaction lain tidak diperbolehkan.'
          : 'Target ini hanya menerima statement DML transaksional (SELECT, INSERT, UPDATE, DELETE). DDL ditolak karena dapat melakukan implicit commit.'}
      </p>
      {validation ? (
        <p className="form-error" role="alert">
          <Icon name="error" size={16} />
          {validation}
        </p>
      ) : null}
      {!disabled ? (
        <div className="migration-editor__actions">
          <button className="button button--secondary" disabled={pending} type="submit">
            {pending ? 'Menyimpan…' : submitLabel}
          </button>
        </div>
      ) : null}
    </form>
  )
}
