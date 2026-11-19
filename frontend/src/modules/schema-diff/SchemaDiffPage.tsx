import { useMutation } from '@tanstack/react-query'
/*
 * FILE INI UNTUK APA (SchemaDiffPage.tsx) - BONUS 2: halaman Schema Diff
 * GUIDEBOOK: §3.3 (bonus) + bonus_explanation.md "Bonus 2"
 * URL: /databases/:databaseId/schema-diff  (tombol "Schema diff" di viewer /
 *      "Bandingkan schema" di Riwayat migration). databaseId di URL = TARGET.
 *
 * ALUR DI LAYAR:
 *   1. Pilih SUMBER: database (hanya yang engine-nya sama dengan target) + schema,
 *      dan pilih schema TARGET. Contoh demo: sumber "staging", target "public".
 *   2. Klik "Bandingkan schema" -> POST /api/v1/schema-diff.
 *   3. Hasil:
 *      - "Schema sudah sama" kalau identical
 *      - ringkasan (jumlah perubahan, tabel, yang destruktif) + catatan backend
 *      - daftar perubahan dengan CHECKBOX (semua tercentang di awal), badge jenis,
 *        nilai lama -> baru, peringatan
 *      - kotak SQL yang BISA DIEDIT
 *      - tombol "Buat draft migration" dan "Salin SQL"
 *   4. "Buat draft migration" pindah ke editor migration dengan nama & SQL
 *      sudah terisi. User masih bisa meninjau/mengubah sebelum menyimpan dan
 *      menjalankan (spek: SQL harus bisa ditinjau & diubah sebelum dieksekusi).
 *
 * STATE PENTING:
 *   selected  : ID perubahan yang dicentang
 *   sql       : isi kotak SQL
 *   sqlEdited : true kalau user sudah mengetik manual di kotak SQL. Kalau true,
 *               mencentang/menghapus centang TIDAK menimpa SQL (supaya editan user
 *               tidak hilang); muncul tombol "Susun ulang dari pilihan" (rebuild).
 *
 * KEAMANAN: halaman ini cuma membaca struktur (backend butuh akses View ke kedua
 * database). Tombol draft hanya muncul kalau user punya View + Edit di target
 * (canDraft), dan saat draft dijalankan, endpoint migration mengecek ulang
 * View + Edit serta aturan SQL (MySQL hanya DML).
 */
import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { EngineBadge } from '@/components/Badges'
import { describeError, poseForError } from '@/components/error-message'
import { Icon, type IconName } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { useAccessibleDatabase, useAccessibleDatabases } from '@/modules/databases/api'
import { buildDiffSQL, compareSchemas } from '@/modules/schema-diff/api'
import type { ChangeKind, SchemaChange, SchemaDiffResult } from '@/modules/schema-diff/types'
import { useSchemas } from '@/modules/viewer/api'
import '@/modules/schema-diff/schema-diff.css'

// Label, ikon, dan warna badge untuk tiap jenis perubahan (Table baru, Ubah kolom, Hapus, ...).
const kindStyles: Record<ChangeKind, { label: string; icon: IconName; className: string }> = {
  create_table: { label: 'Table baru', icon: 'plus', className: 'diff-kind diff-kind--add' },
  add_column: { label: 'Kolom baru', icon: 'plus', className: 'diff-kind diff-kind--add' },
  add_constraint: { label: 'Constraint baru', icon: 'plus', className: 'diff-kind diff-kind--add' },
  create_index: { label: 'Index baru', icon: 'plus', className: 'diff-kind diff-kind--add' },
  alter_column: { label: 'Ubah kolom', icon: 'pencil', className: 'diff-kind diff-kind--alter' },
  drop_table: { label: 'Hapus table', icon: 'trash', className: 'diff-kind diff-kind--drop' },
  drop_column: { label: 'Hapus kolom', icon: 'trash', className: 'diff-kind diff-kind--drop' },
  drop_constraint: { label: 'Hapus constraint', icon: 'trash', className: 'diff-kind diff-kind--drop' },
  drop_index: { label: 'Hapus index', icon: 'trash', className: 'diff-kind diff-kind--drop' },
}

// SchemaDiffPage (bonus): bandingkan schema yang diinginkan dengan schema saat
// ini pada database ini, lalu hasilkan kandidat SQL migration yang dapat diubah.
// SchemaDiffPage: komponen utama halaman.
export function SchemaDiffPage() {
  const { databaseId = '' } = useParams()
  const navigate = useNavigate()
  const target = useAccessibleDatabase(databaseId)
  const databases = useAccessibleDatabases()
  const [sourceDatabaseId, setSourceDatabaseId] = useState(databaseId)
  const [sourceSchema, setSourceSchema] = useState('')
  const [targetSchema, setTargetSchema] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [sql, setSql] = useState('')
  const [sqlEdited, setSqlEdited] = useState(false)
  const [copied, setCopied] = useState(false)

  // compare: kirim perbandingan. Saat sukses: centang SEMUA perubahan, isi kotak SQL
  // dengan SQL dari backend, reset status "diedit manual" dan "tersalin".
  const compare = useMutation({
    mutationFn: () =>
      compareSchemas({ databaseId: sourceDatabaseId, schema: sourceSchema }, { databaseId, schema: targetSchema }),
    onSuccess: (result) => {
      setSelected(new Set(result.changes.map((change) => change.id)))
      setSql(result.sql)
      setSqlEdited(false)
      setCopied(false)
    },
  })

  // Hanya database dengan engine yang sama yang dapat dibandingkan.
  const sources = useMemo(
    () => databases.data?.filter((database) => database.engine === target.data?.engine) ?? [],
    [databases.data, target.data?.engine],
  )

  if (target.isPending) {
    return <StatusMessage tone="loading">Memeriksa akses database...</StatusMessage>
  }
  if (target.isError) {
    return (
      <StatusMessage illustration={poseForError(target.error)} title="Database tidak dapat dibuka" tone="error">
        {describeError(target.error)} <Link to="/databases">Kembali ke daftar database</Link>
      </StatusMessage>
    )
  }

  // result = hasil terakhir. canDraft = boleh dijadikan draft (executable + View+Edit).
  // ready = semua pilihan sudah diisi -> tombol "Bandingkan schema" aktif.
  const result = compare.data
  const canDraft = result?.executable === true && target.data.accessLevel === 'view_edit'
  const ready = sourceDatabaseId !== '' && sourceSchema !== '' && targetSchema !== ''

  // toggle: centang/hapus centang satu perubahan, lalu (kalau SQL belum diedit
  // manual) susun ulang SQL hanya dari perubahan yang tercentang.
  function toggle(result: SchemaDiffResult, id: string) {
    const next = new Set(selected)
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    setSelected(next)
    // SQL disusun ulang dari pilihan, kecuali pengguna sudah mengubahnya manual.
    if (!sqlEdited) {
      setSql(buildDiffSQL(result.changes.filter((change) => next.has(change.id))))
    }
  }

  // rebuild: tombol "Susun ulang dari pilihan" -> buang editan manual, susun dari centang.
  function rebuild(result: SchemaDiffResult) {
    setSql(buildDiffSQL(result.changes.filter((change) => selected.has(change.id))))
    setSqlEdited(false)
  }

  // createDraft: pindah ke halaman draft migration baru, membawa nama otomatis
  // (schema_diff_<schema>_<YYYYMMDD>) dan SQL saat ini lewat router state (prefill).
  // Draft BELUM disimpan; user yang menekan Simpan di editor.
  function createDraft() {
    const stamp = new Date().toISOString().slice(0, 10).replaceAll('-', '')
    void navigate(`/databases/${databaseId}/migrations/new`, {
      state: { prefill: { name: `schema_diff_${targetSchema}_${stamp}`, sql } },
    })
  }

  // copySql: salin SQL ke clipboard; tampilkan "Tersalin" kalau berhasil.
  async function copySql() {
    try {
      await navigator.clipboard.writeText(sql)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <section>
      <div className="page-header">
        <div className="page-header__text">
          <h1>Schema diff</h1>
          <div className="page-header__meta">
            <span className="muted">{target.data.name}</span>
            <EngineBadge engine={target.data.engine} />
          </div>
          <p className="muted">
            Bandingkan schema yang diinginkan dengan schema saat ini, lalu tinjau SQL migration yang dihasilkan.
          </p>
        </div>
        <div className="page-header__actions">
          <Link className="button button--secondary" to={`/databases/${databaseId}/migrations`}>
            <Icon name="history" size={16} />
            Riwayat migration
          </Link>
        </div>
      </div>

      <form
        aria-label="Pilih schema yang dibandingkan"
        className="diff-picker"
        onSubmit={(event) => {
          event.preventDefault()
          if (ready) compare.mutate()
        }}
      >
        <div className="panel diff-side">
          <span className="diff-side__label">
            <Icon name="zap" size={14} />
            Schema yang diinginkan (sumber)
          </span>
          <label className="form-field">
            Database
            <select
              onChange={(event) => {
                setSourceDatabaseId(event.target.value)
                setSourceSchema('')
              }}
              value={sourceDatabaseId}
            >
              {sources.map((database) => (
                <option key={database.id} value={database.id}>
                  {database.name}
                </option>
              ))}
            </select>
          </label>
          <SchemaSelect databaseId={sourceDatabaseId} label="Schema" onChange={setSourceSchema} value={sourceSchema} />
        </div>

        <div aria-hidden="true" className="diff-arrow">
          <Icon name="arrowRight" size={20} />
        </div>

        <div className="panel diff-side">
          <span className="diff-side__label">
            <Icon name="database" size={14} />
            Schema saat ini (target)
          </span>
          <label className="form-field">
            Database
            <select disabled value={databaseId}>
              <option value={databaseId}>{target.data.name}</option>
            </select>
          </label>
          <SchemaSelect databaseId={databaseId} label="Schema target" onChange={setTargetSchema} value={targetSchema} />
        </div>

        <div className="diff-picker__actions">
          <button className="button button--primary" disabled={!ready || compare.isPending} type="submit">
            <Icon className={compare.isPending ? 'spinner' : undefined} name={compare.isPending ? 'loader' : 'search'} size={16} />
            {compare.isPending ? 'Membandingkan...' : 'Bandingkan schema'}
          </button>
        </div>
      </form>

      {compare.isError ? (
        <StatusMessage title="Perbandingan gagal" tone="error">
          {describeError(compare.error)}
        </StatusMessage>
      ) : null}

      {result?.identical ? (
        <StatusMessage illustration="success" title="Schema sudah sama" tone="success">
          Tidak ada perbedaan antara {result.source.schema} dan {result.target.schema}. Tidak perlu migration.
        </StatusMessage>
      ) : null}

      {result && !result.identical ? (
        <div className="diff-result">
          <div className="diff-summary">
            <span className="badge badge--info">{result.summary.total} perubahan</span>
            <span className="badge">{result.summary.tables} table</span>
            {result.summary.destructive > 0 ? (
              <span className="badge badge--danger">
                <Icon name="alert" size={12} />
                {result.summary.destructive} destruktif
              </span>
            ) : null}
          </div>
          {result.notes.map((note) => (
            <StatusMessage key={note} tone={note.includes('destruktif') || note.includes('MySQL') ? 'warning' : 'info'}>
              {note}
            </StatusMessage>
          ))}

          <div className="diff-columns">
            <div className="panel">
              <div className="panel__header">
                <h2>Perbedaan</h2>
                <span className="muted">
                  {selected.size}/{result.changes.length} dipilih
                </span>
              </div>
              <ul className="diff-changes">
                {result.changes.map((change) => (
                  <ChangeItem
                    change={change}
                    checked={selected.has(change.id)}
                    key={change.id}
                    onToggle={() => toggle(result, change.id)}
                  />
                ))}
              </ul>
            </div>

            <div className="panel diff-sql">
              <div className="panel__header">
                <h2>SQL migration</h2>
                {sqlEdited ? (
                  <button className="button button--ghost button--small" onClick={() => rebuild(result)} type="button">
                    <Icon name="refresh" size={14} />
                    Susun ulang dari pilihan
                  </button>
                ) : null}
              </div>
              <label className="visually-hidden" htmlFor="diff-sql">
                SQL migration hasil schema diff
              </label>
              <textarea
                id="diff-sql"
                onChange={(event) => {
                  setSql(event.target.value)
                  setSqlEdited(true)
                }}
                spellCheck={false}
                value={sql}
              />
              <p className="muted">SQL dapat diubah sebelum dijadikan draft. Draft tetap dapat ditinjau lagi di editor migration.</p>
              <div className="form-actions">
                {canDraft ? (
                  <button className="button button--primary" disabled={sql.trim() === ''} onClick={createDraft} type="button">
                    <Icon name="plus" size={16} />
                    Buat draft migration
                  </button>
                ) : null}
                <button className="button button--secondary" disabled={sql.trim() === ''} onClick={() => void copySql()} type="button">
                  <Icon name={copied ? 'check' : 'copy'} size={16} />
                  {copied ? 'Tersalin' : 'Salin SQL'}
                </button>
              </div>
              {!canDraft && result.executable ? (
                <p className="muted">Butuh akses View + Edit pada database ini untuk membuat draft migration.</p>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}

// ChangeItem: satu baris perubahan -> checkbox, badge jenis, deskripsi,
// sebelum -> sesudah, peringatan (dan tanda destruktif).
function ChangeItem({ change, checked, onToggle }: { change: SchemaChange; checked: boolean; onToggle: () => void }) {
  const style = kindStyles[change.kind]
  return (
    <li className={checked ? 'diff-change' : 'diff-change diff-change--off'}>
      <label className="diff-change__head">
        <input checked={checked} onChange={onToggle} type="checkbox" />
        <span className={style.className}>
          <Icon name={style.icon} size={12} />
          {style.label}
        </span>
        <span className="diff-change__title">{change.description}</span>
        {change.destructive ? (
          <span className="badge badge--danger">
            <Icon name="alert" size={12} />
            Destruktif
          </span>
        ) : null}
      </label>
      {change.before || change.after ? (
        <div className="diff-change__values">
          {change.before ? <code className="diff-before">{change.before}</code> : null}
          {change.before && change.after ? <Icon name="arrowRight" size={14} /> : null}
          {change.after ? <code className="diff-after">{change.after}</code> : null}
        </div>
      ) : null}
      {change.warning ? (
        <p className="diff-change__warning">
          <Icon name="alert" size={13} />
          {change.warning}
        </p>
      ) : null}
    </li>
  )
}

interface SchemaSelectProps {
  databaseId: string
  label: string
  value: string
  onChange: (schema: string) => void
}

// SchemaSelect: dropdown schema untuk satu database (memakai useSchemas milik viewer).
function SchemaSelect({ databaseId, label, value, onChange }: SchemaSelectProps) {
  const schemas = useSchemas(databaseId)
  return (
    <div className="form-field">
      <label className="form-field">
        {label}
        <select disabled={!schemas.data} onChange={(event) => onChange(event.target.value)} value={value}>
          <option disabled value="">
            {schemas.isPending ? 'Memuat schema...' : schemas.isError ? 'Schema gagal dimuat' : 'Pilih schema'}
          </option>
          {schemas.data?.map((schema) => (
            <option key={schema.name} value={schema.name}>
              {schema.name}
            </option>
          ))}
        </select>
      </label>
      {/* Error tidak boleh hanya jadi placeholder abu-abu: beri alasan dan jalan keluar. */}
      {schemas.isError ? (
        <div className="field-error" role="alert">
          <Icon name="error" size={14} />
          <span>{describeError(schemas.error)}</span>
          <button
            className="button button--ghost button--small"
            disabled={schemas.isFetching}
            onClick={() => void schemas.refetch()}
            type="button"
          >
            <Icon className={schemas.isFetching ? 'spinner' : undefined} name="refresh" size={14} />
            Coba lagi
          </button>
        </div>
      ) : null}
    </div>
  )
}
