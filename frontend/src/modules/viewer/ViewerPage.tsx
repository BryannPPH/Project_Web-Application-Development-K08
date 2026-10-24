import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { AccessBadge, EngineBadge } from '@/components/Badges'
import { describeError, poseForError } from '@/components/error-message'
import { Icon, type IconName } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { useAccessibleDatabase, useAccessibleDatabases } from '@/modules/databases/api'
import { useSchemas, useTableDetail, useTables } from '@/modules/viewer/api'
import { TableData } from '@/modules/viewer/TableData'
import { TableStructure } from '@/modules/viewer/TableStructure'
import { tableTypeLabels, type TableType } from '@/modules/viewer/types'
import '@/modules/viewer/viewer.css'

function schemaUrl(databaseId: string, schema: string) {
  return `/databases/${databaseId}/schemas/${encodeURIComponent(schema)}`
}

function tableUrl(databaseId: string, schema: string, table: string) {
  return `${schemaUrl(databaseId, schema)}/tables/${encodeURIComponent(table)}`
}

const tableTypeIcons: Record<TableType, IconName> = {
  table: 'table',
  partitioned_table: 'table',
  view: 'eye',
  materialized_view: 'eye',
  foreign_table: 'link',
  unknown: 'table',
}

const rowFormat = new Intl.NumberFormat('id-ID')

// ViewerPage: navigasi schema -> table -> detail/data untuk satu database.
export function ViewerPage() {
  const { databaseId = '', schema, table } = useParams()
  const database = useAccessibleDatabase(databaseId)

  if (database.isPending) {
    return <StatusMessage tone="loading">Memeriksa akses database…</StatusMessage>
  }
  if (database.isError) {
    return (
      <StatusMessage illustration={poseForError(database.error)} title="Database tidak dapat dibuka" tone="error">
        {describeError(database.error)}{' '}
        <Link to="/databases">Kembali ke daftar database</Link>
      </StatusMessage>
    )
  }

  return (
    <section>
      <div className="page-header">
        <div className="page-header__text">
          <h1>{database.data.name}</h1>
          <div className="page-header__meta">
            <code className="mono muted">{database.data.databaseName}</code>
            <EngineBadge engine={database.data.engine} />
            <AccessBadge level={database.data.accessLevel} />
          </div>
        </div>
        <div className="page-header__actions">
          <DatabaseSwitcher currentId={databaseId} />
          <Link className="button button--secondary" to={`/databases/${databaseId}/schema-diff`}>
            <Icon name="compare" size={16} />
            Schema diff
          </Link>
          <Link className="button button--secondary" to={`/databases/${databaseId}/migrations`}>
            <Icon name="history" size={16} />
            Riwayat migration
          </Link>
        </div>
      </div>

      <ViewerWorkspace databaseId={databaseId} schema={schema} table={table} />
    </section>
  )
}

interface ViewerWorkspaceProps {
  databaseId: string
  schema?: string
  table?: string
}

// Bila daftar schema gagal (mis. database target mati), tampilkan error selebar
// halaman bersama Senja alih-alih kotak kecil di sidebar.
function ViewerWorkspace({ databaseId, schema, table }: ViewerWorkspaceProps) {
  const schemas = useSchemas(databaseId)

  if (schemas.isError) {
    return (
      <StatusMessage
        actionLabel="Coba lagi"
        illustration={poseForError(schemas.error)}
        onAction={() => void schemas.refetch()}
        title="Schema gagal dimuat"
        tone="error"
      >
        {describeError(schemas.error)}
      </StatusMessage>
    )
  }

  return (
    <div className="viewer-layout">
      <aside className="viewer-sidebar" aria-label="Navigasi schema dan table">
        <SchemaNavigation databaseId={databaseId} selectedSchema={schema} selectedTable={table} />
      </aside>
      <div className="viewer-content">
        {schema && table ? (
          <TablePanel databaseId={databaseId} schema={schema} table={table} />
        ) : (
          <StatusMessage
            illustration="lantern"
            title={schema ? 'Pilih table' : 'Pilih schema'}
            tone="empty"
          >
            {schema ? 'Pilih table untuk melihat struktur dan data.' : 'Pilih schema di panel kiri.'}
          </StatusMessage>
        )}
      </div>
    </div>
  )
}

function DatabaseSwitcher({ currentId }: { currentId: string }) {
  const databases = useAccessibleDatabases()
  const navigate = useNavigate()

  if (!databases.data || databases.data.length < 2) {
    return null
  }
  return (
    <label className="form-field viewer-switcher">
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
    return <StatusMessage tone="loading">Memuat schema…</StatusMessage>
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
    return (
      <StatusMessage illustration="searching" tone="empty">
        Tidak ada schema yang dapat ditampilkan.
      </StatusMessage>
    )
  }

  return (
    <nav>
      <h2 className="viewer-sidebar__title">Schema</h2>
      <ul className="viewer-tree">
        {schemas.data.map((item) => {
          const open = item.name === selectedSchema
          return (
            <li key={item.name}>
              <Link
                aria-current={open && !selectedTable ? 'page' : undefined}
                className={open ? 'viewer-tree__schema viewer-tree__schema--open' : 'viewer-tree__schema'}
                to={schemaUrl(databaseId, item.name)}
              >
                <Icon name={open ? 'chevronDown' : 'chevronRight'} size={14} />
                <Icon name="folder" size={15} />
                <span className="viewer-tree__label">{item.name}</span>
              </Link>
              {open ? (
                <div className="viewer-tables">
                  {tables.isPending ? <p className="muted viewer-tree__note">Memuat table…</p> : null}
                  {tables.isError ? (
                    <p className="muted viewer-tree__note" role="alert">
                      {describeError(tables.error)}
                    </p>
                  ) : null}
                  {tables.data?.length === 0 ? <p className="muted viewer-tree__note">Schema kosong.</p> : null}
                  <ul className="viewer-tree">
                    {tables.data?.map((tableItem) => (
                      <li key={tableItem.name}>
                        <Link
                          aria-current={tableItem.name === selectedTable ? 'page' : undefined}
                          className="viewer-tree__table"
                          title={`${tableTypeLabels[tableItem.type]} · ~${rowFormat.format(tableItem.estimatedRows)} baris`}
                          to={tableUrl(databaseId, item.name, tableItem.name)}
                        >
                          <Icon name={tableTypeIcons[tableItem.type]} size={14} />
                          <span className="viewer-tree__label">{tableItem.name}</span>
                          {tableItem.type !== 'table' ? (
                            <span aria-hidden="true" className="viewer-tree__type">
                              {tableTypeLabels[tableItem.type]}
                            </span>
                          ) : null}
                        </Link>
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </li>
          )
        })}
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
    <div className="panel viewer-panel">
      <div className="viewer-panel__header">
        <h2>
          <code>
            {schema}.{table}
          </code>
        </h2>
        {detail.data ? (
          <span className="muted">
            {tableTypeLabels[detail.data.table.type]} · ~{rowFormat.format(detail.data.table.estimatedRows)} baris
          </span>
        ) : null}
        <span className="badge viewer-panel__readonly">
          <Icon name="lock" size={12} />
          Hanya baca
        </span>
      </div>
      <div className="tab-list" role="tablist">
        <button aria-selected={tab === 'structure'} onClick={() => selectTab('structure')} role="tab" type="button">
          <Icon name="table" size={15} />
          Struktur
        </button>
        <button aria-selected={tab === 'data'} onClick={() => selectTab('data')} role="tab" type="button">
          <Icon name="database" size={15} />
          Data
        </button>
      </div>

      {tab === 'structure' ? (
        <>
          {detail.isPending ? <StatusMessage tone="loading">Memuat struktur table…</StatusMessage> : null}
          {detail.isError ? (
            <StatusMessage illustration={poseForError(detail.error)} title="Struktur gagal dimuat" tone="error">
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
