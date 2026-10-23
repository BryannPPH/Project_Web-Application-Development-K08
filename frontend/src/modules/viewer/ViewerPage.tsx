import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { describeError } from '@/components/error-message'
import { StatusMessage } from '@/components/StatusMessage'
import { useAccessibleDatabase, useAccessibleDatabases } from '@/modules/databases/api'
import { accessLevelLabel } from '@/modules/databases/types'
import { useSchemas, useTableDetail, useTables } from '@/modules/viewer/api'
import { TableData } from '@/modules/viewer/TableData'
import { TableStructure } from '@/modules/viewer/TableStructure'
import { tableTypeLabels } from '@/modules/viewer/types'
import '@/modules/viewer/viewer.css'

function schemaUrl(databaseId: string, schema: string) {
  return `/databases/${databaseId}/schemas/${encodeURIComponent(schema)}`
}

function tableUrl(databaseId: string, schema: string, table: string) {
  return `${schemaUrl(databaseId, schema)}/tables/${encodeURIComponent(table)}`
}

// ViewerPage: navigasi schema -> table -> detail/data untuk satu database.
export function ViewerPage() {
  const { databaseId = '', schema, table } = useParams()
  const database = useAccessibleDatabase(databaseId)

  if (database.isPending) {
    return <StatusMessage>Memeriksa akses database…</StatusMessage>
  }
  if (database.isError) {
    return (
      <StatusMessage title="Database tidak dapat dibuka" tone="error">
        {describeError(database.error)} <Link to="/databases">Kembali ke daftar database</Link>
      </StatusMessage>
    )
  }

  return (
    <section>
      <div className="page-header">
        <div>
          <h1>{database.data.name}</h1>
          <span className="muted">
            <code>{database.data.databaseName}</code> ·{' '}
            <span className="badge">{accessLevelLabel(database.data.accessLevel)}</span>
          </span>
        </div>
        <DatabaseSwitcher currentId={databaseId} />
      </div>

      <div className="viewer-layout">
        <aside className="viewer-sidebar" aria-label="Navigasi schema dan table">
          <SchemaNavigation databaseId={databaseId} selectedSchema={schema} selectedTable={table} />
        </aside>
        <div className="viewer-content">
          {schema && table ? (
            <TablePanel databaseId={databaseId} schema={schema} table={table} />
          ) : (
            <StatusMessage tone="empty">
              {schema ? 'Pilih table untuk melihat struktur dan data.' : 'Pilih schema di panel kiri.'}
            </StatusMessage>
          )}
        </div>
      </div>
    </section>
  )
}

function DatabaseSwitcher({ currentId }: { currentId: string }) {
  const databases = useAccessibleDatabases()
  const navigate = useNavigate()

  if (!databases.data || databases.data.length < 2) {
    return null
  }
  return (
    <label className="form-field">
      Ganti database
      <select
        onChange={(event) => void navigate(`/databases/${event.target.value}`)}
        value={currentId}
      >
        {databases.data.map((database) => (
          <option key={database.id} value={database.id}>
            {database.name}
          </option>
        ))}
      </select>
    </label>
  )
}

interface SchemaNavigationProps {
  databaseId: string
  selectedSchema?: string
  selectedTable?: string
}

function SchemaNavigation({ databaseId, selectedSchema, selectedTable }: SchemaNavigationProps) {
  const schemas = useSchemas(databaseId)
  const tables = useTables(databaseId, selectedSchema)

  if (schemas.isPending) {
    return <StatusMessage>Memuat schema…</StatusMessage>
  }
  if (schemas.isError) {
    return (
      <StatusMessage
        actionLabel="Coba lagi"
        onAction={() => void schemas.refetch()}
        title="Schema gagal dimuat"
        tone="error"
      >
        {describeError(schemas.error)}
      </StatusMessage>
    )
  }
  if (schemas.data.length === 0) {
    return <StatusMessage tone="empty">Tidak ada schema yang dapat ditampilkan.</StatusMessage>
  }

  return (
    <nav>
      <h2 className="viewer-sidebar__title">Schema</h2>
      <ul className="viewer-list">
        {schemas.data.map((item) => (
          <li key={item.name}>
            <Link
              aria-current={item.name === selectedSchema ? 'page' : undefined}
              to={schemaUrl(databaseId, item.name)}
            >
              {item.name}
            </Link>
            {item.name === selectedSchema ? (
              <div className="viewer-tables">
                {tables.isPending ? <p className="muted">Memuat table…</p> : null}
                {tables.isError ? (
                  <p className="muted" role="alert">
                    {describeError(tables.error)}
                  </p>
                ) : null}
                {tables.data?.length === 0 ? <p className="muted">Schema kosong.</p> : null}
                <ul className="viewer-list">
                  {tables.data?.map((tableItem) => (
                    <li key={tableItem.name}>
                      <Link
                        aria-current={tableItem.name === selectedTable ? 'page' : undefined}
                        to={tableUrl(databaseId, item.name, tableItem.name)}
                      >
                        {tableItem.name}
                      </Link>
                      {tableItem.type !== 'table' ? (
                        <span className="badge">{tableTypeLabels[tableItem.type]}</span>
                      ) : null}
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </li>
        ))}
      </ul>
    </nav>
  )
}

function TablePanel({ databaseId, schema, table }: { databaseId: string; schema: string; table: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = searchParams.get('tab') === 'data' ? 'data' : 'structure'
  const detail = useTableDetail(databaseId, schema, table)

  function selectTab(next: 'structure' | 'data') {
    const params = new URLSearchParams(searchParams)
    params.set('tab', next)
    setSearchParams(params)
  }

  return (
    <div className="panel">
      <h2>
        <code>
          {schema}.{table}
        </code>
      </h2>
      <div className="tab-list" role="tablist">
        <button aria-selected={tab === 'structure'} onClick={() => selectTab('structure')} role="tab" type="button">
          Struktur
        </button>
        <button aria-selected={tab === 'data'} onClick={() => selectTab('data')} role="tab" type="button">
          Data
        </button>
      </div>

      {tab === 'structure' ? (
        <>
          {detail.isPending ? <StatusMessage>Memuat struktur table…</StatusMessage> : null}
          {detail.isError ? (
            <StatusMessage title="Struktur gagal dimuat" tone="error">
              {describeError(detail.error)}
            </StatusMessage>
          ) : null}
          {detail.data ? <TableStructure detail={detail.data} /> : null}
        </>
      ) : (
        <TableData databaseId={databaseId} schema={schema} table={table} />
      )}
    </div>
  )
}
