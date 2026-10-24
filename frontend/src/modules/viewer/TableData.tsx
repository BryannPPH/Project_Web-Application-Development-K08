import { useSearchParams } from 'react-router-dom'

import { describeError, poseForError } from '@/components/error-message'
import { Icon } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { parsePageParams, useTableRows } from '@/modules/viewer/api'

function renderCell(value: unknown) {
  if (value === null || value === undefined) {
    return <span className="cell-null">NULL</span>
  }
  if (typeof value === 'object') {
    return <code>{JSON.stringify(value)}</code>
  }
  return String(value)
}

interface TableDataProps {
  databaseId: string
  schema: string
  table: string
}

// TableData menyimpan limit/offset di URL agar reload dan tombol back
// mempertahankan halaman serta database yang dipilih.
export function TableData({ databaseId, schema, table }: TableDataProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const { limit, offset } = parsePageParams(searchParams)
  const rows = useTableRows(databaseId, schema, table, limit, offset)

  function goTo(nextOffset: number, nextLimit = limit) {
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'data')
    next.set('limit', String(nextLimit))
    next.set('offset', String(Math.max(0, nextOffset)))
    setSearchParams(next)
  }

  if (rows.isPending) {
    return <StatusMessage tone="loading">Memuat data…</StatusMessage>
  }
  if (rows.isError) {
    return (
      <StatusMessage
        actionLabel="Coba lagi"
        illustration={poseForError(rows.error)}
        onAction={() => void rows.refetch()}
        title="Data gagal dimuat"
        tone="error"
      >
        {describeError(rows.error)}
      </StatusMessage>
    )
  }

  const page = rows.data
  return (
    <div>
      {page.rows.length === 0 ? (
        <StatusMessage illustration="searching" tone="empty">
          {offset === 0 ? 'Table ini belum memiliki data.' : 'Tidak ada data pada halaman ini.'}
        </StatusMessage>
      ) : (
        <div className={rows.isFetching ? 'data-table-wrapper data-grid data-grid--fetching' : 'data-table-wrapper data-grid'}>
          <table className="data-table">
            <thead>
              <tr>
                {page.columns.map((column) => (
                  <th key={column.name} title={column.dataType}>
                    {column.name}
                    <small>{column.dataType}</small>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {page.rows.map((row, rowIndex) => (
                <tr key={offset + rowIndex}>
                  {row.map((value, columnIndex) => (
                    <td
                      key={page.columns[columnIndex]?.name ?? columnIndex}
                      title={typeof value === 'string' && value.length > 40 ? value : undefined}
                    >
                      {renderCell(value)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {page.truncated ? (
        <StatusMessage tone="warning">Sebagian nilai panjang dipotong agar tampilan tetap ringan.</StatusMessage>
      ) : null}
      {page.orderedBy.length === 0 && page.rows.length > 0 ? (
        <p className="muted">Table tanpa primary key: urutan baris tidak dijamin.</p>
      ) : null}

      <div className="pagination">
        <span className="muted">
          {page.rows.length > 0
            ? `Baris ${offset + 1}–${offset + page.rows.length}`
            : `Offset ${offset}`}
          {page.orderedBy.length > 0 ? ` · diurutkan menurut ${page.orderedBy.join(', ')}` : ''}
        </span>
        <div className="inline-actions">
          <label className="form-field form-field--inline">
            Per halaman
            <select
              aria-label="Jumlah baris per halaman"
              onChange={(event) => goTo(0, Number(event.target.value))}
              value={limit}
            >
              {[25, 50, 100].map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </label>
          <button
            className="button button--secondary button--small"
            disabled={offset === 0 || rows.isFetching}
            onClick={() => goTo(offset - limit)}
            type="button"
          >
            <Icon name="chevronLeft" size={14} />
            Sebelumnya
          </button>
          <button
            className="button button--secondary button--small"
            disabled={!page.hasMore || rows.isFetching}
            onClick={() => goTo(offset + limit)}
            type="button"
          >
            Berikutnya
            <Icon name="chevronRight" size={14} />
          </button>
        </div>
      </div>
    </div>
  )
}
