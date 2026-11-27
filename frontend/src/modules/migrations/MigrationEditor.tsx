import { useEffect, useId, useState } from 'react'

import { Icon } from '@/components/Icon'
import type { DraftInput, ExecutionMode } from '@/modules/migrations/types'

const MAX_SQL_BYTES = 256 * 1024

interface MigrationEditorProps {
  initialValue?: DraftInput
  disabled?: boolean
  pending?: boolean
  submitLabel: string
  // true bila simpan adalah aksi utama halaman (buat draft); false bila ada aksi lain yang lebih utama (Jalankan).
  primary?: boolean
  onSubmit: (input: DraftInput) => void
  // false untuk target (mis. MySQL) yang melakukan implicit commit pada DDL.
  transactionalDdl?: boolean
  supportsBatch?: boolean
  // Dipanggil setiap SQL berubah, mis. agar panel EXPLAIN memakai SQL terbaru.
  onSqlChange?: (sql: string) => void
  onDirtyChange?: (dirty: boolean) => void
}

function formValues(input: DraftInput) {
  return {
    name: input.name,
    sql: input.sql,
    irreversible: Boolean(input.irreversibleReason),
    downSql: input.downSql ?? '',
    reason: input.irreversibleReason ?? '',
    mode: input.executionMode ?? 'single',
    selector: input.batchSelectSql ?? '',
    size: String(input.batchSize ?? 1000),
    cursor: String(input.batchCursorStart ?? 0),
  }
}

export function MigrationEditor({
  initialValue = { name: '', sql: '' },
  disabled = false,
  primary = false,
  pending = false,
  submitLabel,
  onSubmit,
  transactionalDdl = true,
  supportsBatch = true,
  onSqlChange,
  onDirtyChange,
}: MigrationEditorProps) {
  const baseline = JSON.stringify(formValues(initialValue))
  const [form, setForm] = useState(() => formValues(initialValue))
  const { name, sql, irreversible, downSql, reason, mode, selector, size, cursor } = form
  const [validation, setValidation] = useState<string | null>(null)
  const sqlId = useId()
  const locked = disabled || pending
  const lineCount = Math.max(sql.split('\n').length, 12)
  const dirty = JSON.stringify(form) !== baseline
  useEffect(() => onDirtyChange?.(dirty), [dirty, onDirtyChange])

  function update<K extends keyof typeof form>(field: K, value: (typeof form)[K]) {
    setForm((current) => ({ ...current, [field]: value }))
  }

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (locked) return
    if (!name.trim() || !sql.trim()) {
      setValidation('Nama dan SQL migration wajib diisi.')
      return
    }
    const definition = irreversible ? { irreversibleReason: reason.trim() } : { downSql }
    if (irreversible ? !reason.trim() : !downSql.trim()) {
      setValidation(irreversible ? 'Alasan irreversible wajib diisi.' : 'Isi Down SQL atau tandai migration sebagai irreversible.')
      return
    }
    if (mode === 'batch' && (!supportsBatch || !selector.trim() || !size.trim() || !cursor.trim() ||
      !Number.isInteger(Number(size)) || Number(size) < 1 || Number(size) > 10000 || !Number.isSafeInteger(Number(cursor)))) {
      setValidation('Mode batch membutuhkan PostgreSQL, selector, ukuran 1–10.000, dan cursor awal bilangan bulat.')
      return
    }
    const queries = [sql, ...(irreversible ? [] : [downSql]), ...(mode === 'batch' ? [selector] : [])]
    if (name.trim().length > 160 || queries.some((query) => new TextEncoder().encode(query).byteLength > MAX_SQL_BYTES)) {
      setValidation('Nama maksimal 160 karakter dan setiap kolom SQL maksimal 256 KiB.')
      return
    }
    setValidation(null)
    const batch = mode === 'batch' ? { batchSelectSql: selector, batchSize: Number(size), batchCursorStart: Number(cursor) } : {}
    onSubmit({ name: name.trim(), sql, executionMode: mode, ...definition, ...batch })
  }

  return (
    <form className="panel migration-editor" onSubmit={submit}>
      <label className="form-field">
        Nama migration
        <input
          disabled={locked}
          maxLength={160}
          onChange={(event) => update('name', event.target.value)}
          placeholder="contoh: add_loyalty_points"
          value={name}
        />
      </label>
      <div className="form-field">
        <label htmlFor={sqlId}>Up SQL</label>
        <div className="sql-editor">
          <div aria-hidden="true" className="sql-editor__gutter">
            {Array.from({ length: lineCount }, (_, index) => (
              <span key={index}>{index + 1}</span>
            ))}
          </div>
          <textarea
            aria-describedby="sql-help"
            disabled={locked}
            id={sqlId}
            onChange={(event) => {
              update('sql', event.target.value)
              onSqlChange?.(event.target.value)
            }}
            rows={lineCount}
            spellCheck={false}
            value={sql}
          />
        </div>
      </div>
      <label className="form-field">
        Strategi rollback
        <select disabled={locked} onChange={(event) => update('irreversible', event.target.value === 'irreversible')} value={irreversible ? 'irreversible' : 'reversible'}>
          <option value="reversible">Bisa dibatalkan dengan Down SQL</option>
          <option value="irreversible">Irreversible</option>
        </select>
      </label>
      {irreversible ? (
        <label className="form-field">
          Alasan irreversible
          <textarea disabled={locked} maxLength={1000} onChange={(event) => update('reason', event.target.value)} rows={3} value={reason} />
        </label>
      ) : (
        <label className="form-field">
          Down SQL
          <textarea className="migration-sql-field" disabled={locked} onChange={(event) => update('downSql', event.target.value)} rows={5} spellCheck={false} value={downSql} />
        </label>
      )}
      <label className="form-field">
        Mode eksekusi
        <select disabled={locked} onChange={(event) => update('mode', event.target.value as ExecutionMode)} value={mode}>
          <option value="single">Biasa</option>
          <option disabled={!supportsBatch} value="batch">Batch (PostgreSQL)</option>
        </select>
      </label>
      {mode === 'batch' ? (
        <>
          <label className="form-field">
            SQL selector batch
            <textarea className="migration-sql-field" disabled={locked} onChange={(event) => update('selector', event.target.value)} rows={5} spellCheck={false} value={selector} />
          </label>
          <div className="migration-batch-fields">
            <label className="form-field">
              Ukuran batch
              <input disabled={locked} max={10000} min={1} onChange={(event) => update('size', event.target.value)} step={1} type="number" value={size} />
            </label>
            <label className="form-field">
              Cursor awal
              <input disabled={locked} onChange={(event) => update('cursor', event.target.value)} step={1} type="number" value={cursor} />
            </label>
          </div>
        </>
      ) : null}
      <p className="migration-help" id="sql-help">
        <Icon name="info" size={14} />
        {mode === 'batch'
          ? 'Selector menerima $1 (cursor terakhir) dan $2 (ukuran batch). Up SQL dan Down SQL menerima batas bawah eksklusif ($1) dan batas atas inklusif ($2). Batasi keduanya ke cakupan data yang sudah ditentukan. Setiap batch di-commit terpisah.'
          : transactionalDdl
          ? 'Seluruh SQL dijalankan dalam satu transaction. BEGIN, COMMIT, ROLLBACK, dan kontrol transaction lain tidak diperbolehkan.'
          : 'DML (SELECT, INSERT, UPDATE, DELETE) dijalankan dalam satu transaksi. DDL didukung terpisah, tetapi melakukan implicit commit dan tidak dapat di-rollback bersama statement lain. Pisahkan DDL dan DML ke migration berbeda.'}
      </p>
      {validation ? (
        <p className="form-error" role="alert">
          <Icon name="error" size={16} />
          {validation}
        </p>
      ) : null}
      {!disabled ? (
        <div className="migration-editor__actions">
          <button className={primary ? 'button button--primary' : 'button button--secondary'} disabled={pending} type="submit">
            {pending ? 'Menyimpan...' : submitLabel}
          </button>
        </div>
      ) : null}
    </form>
  )
}
