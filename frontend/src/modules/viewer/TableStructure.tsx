import type { TableDetail } from '@/modules/viewer/types'
import { constraintTypeLabels } from '@/modules/viewer/types'

export function TableStructure({ detail }: { detail: TableDetail }) {
  return (
    <div>
      <h3>Column</h3>
      <div className="data-table-wrapper">
        <table className="data-table">
          <thead>
            <tr>
              <th>#</th>
              <th>Nama</th>
              <th>Tipe data</th>
              <th>Nullable</th>
              <th>Default</th>
            </tr>
          </thead>
          <tbody>
            {detail.columns.map((column) => (
              <tr key={column.name}>
                <td>{column.position}</td>
                <td>
                  <code>{column.name}</code>
                </td>
                <td>
                  <code>{column.dataType}</code>
                  {column.identity ? <span className="badge"> identity {column.identity}</span> : null}
                  {column.generated ? <span className="badge"> generated</span> : null}
                </td>
                <td>{column.nullable ? 'Ya' : 'Tidak'}</td>
                <td>
                  {column.default === null ? (
                    <span className="cell-null">—</span>
                  ) : (
                    <code>{column.default}</code>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <h3>Constraint</h3>
      {detail.constraints.length === 0 ? (
        <p className="muted">Table ini tidak memiliki constraint.</p>
      ) : (
        <div className="data-table-wrapper">
          <table className="data-table">
            <thead>
              <tr>
                <th>Nama</th>
                <th>Jenis</th>
                <th>Kolom</th>
                <th>Definisi</th>
              </tr>
            </thead>
            <tbody>
              {detail.constraints.map((constraint) => (
                <tr key={constraint.name}>
                  <td>
                    <code>{constraint.name}</code>
                  </td>
                  <td>
                    <span className="badge">{constraintTypeLabels[constraint.type]}</span>
                  </td>
                  <td>{constraint.columns.join(', ') || '—'}</td>
                  <td>
                    <code>{constraint.definition}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <h3>Index</h3>
      {detail.indexes.length === 0 ? (
        <p className="muted">Table ini tidak memiliki index.</p>
      ) : (
        <div className="data-table-wrapper">
          <table className="data-table">
            <thead>
              <tr>
                <th>Nama</th>
                <th>Unik</th>
                <th>Metode</th>
                <th>Definisi</th>
              </tr>
            </thead>
            <tbody>
              {detail.indexes.map((index) => (
                <tr key={index.name}>
                  <td>
                    <code>{index.name}</code>
                    {index.primary ? <span className="badge"> primary</span> : null}
                  </td>
                  <td>{index.unique ? 'Ya' : 'Tidak'}</td>
                  <td>{index.method}</td>
                  <td>
                    <code>{index.definition}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
