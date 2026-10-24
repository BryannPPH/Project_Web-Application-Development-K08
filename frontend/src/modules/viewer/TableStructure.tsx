import { Icon } from '@/components/Icon'
import type { TableDetail } from '@/modules/viewer/types'
import { constraintTypeLabels } from '@/modules/viewer/types'

export function TableStructure({ detail }: { detail: TableDetail }) {
  const primaryColumns = new Set(
    detail.constraints.filter((item) => item.type === 'primary_key').flatMap((item) => item.columns),
  )
  const foreignColumns = new Set(
    detail.constraints.filter((item) => item.type === 'foreign_key').flatMap((item) => item.columns),
  )

  return (
    <div>
      <h3 className="section-title">Column</h3>
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
                <td className="cell-empty">{column.position}</td>
                <td>
                  <code>{column.name}</code>
                  {primaryColumns.has(column.name) ? (
                    <span title="Primary key">
                      <Icon className="key-icon" name="key" size={13} />
                    </span>
                  ) : null}
                  {foreignColumns.has(column.name) ? (
                    <span title="Foreign key">
                      <Icon className="key-icon key-icon--fk" name="link" size={13} />
                    </span>
                  ) : null}
                </td>
                <td>
                  <code>{column.dataType}</code>{' '}
                  {column.identity ? <span className="badge badge--info">identity {column.identity}</span> : null}
                  {column.generated ? <span className="badge badge--info">generated</span> : null}
                </td>
                <td>{column.nullable ? 'Ya' : 'Tidak'}</td>
                <td>
                  {column.default === null ? (
                    <span className="cell-empty">—</span>
                  ) : (
                    <code>{column.default}</code>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <h3 className="section-title">Constraint</h3>
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
                    <span className={constraint.type === 'primary_key' ? 'badge badge--brand' : 'badge'}>
                      {constraintTypeLabels[constraint.type]}
                    </span>
                  </td>
                  <td>{constraint.columns.join(', ') || '—'}</td>
                  <td>
                    <code className="definition">{constraint.definition}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <h3 className="section-title">Index</h3>
      {detail.indexes.length === 0 ? (
        <p className="muted">Tidak ada index.</p>
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
                    <code>{index.name}</code>{' '}
                    {index.primary ? <span className="badge badge--brand">primary</span> : null}
                  </td>
                  <td>{index.unique ? 'Ya' : 'Tidak'}</td>
                  <td>{index.method}</td>
                  <td>
                    <code className="definition">{index.definition}</code>
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
