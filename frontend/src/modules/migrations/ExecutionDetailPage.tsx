import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'

import { describeError } from '@/components/error-message'
import { StatusMessage } from '@/components/StatusMessage'
import { getExecution } from '@/modules/migrations/api'
import { displayTime, executionActionText, executionStatusText } from '@/modules/migrations/query-helpers'
import '@/modules/migrations/migrations.css'

const eventText: Record<string, string> = {
  started: 'Dimulai',
  completed: 'Selesai',
  failed: 'Gagal',
  interrupted: 'Terputus',
  needs_reconciliation: 'Perlu rekonsiliasi',
  batch_committed: 'Batch committed',
  batch_reverted: 'Batch dibatalkan',
  batch_failed: 'Batch gagal',
  partial: 'Berhenti sebagian',
  partially_reverted: 'Rollback berhenti sebagian',
}

export function ExecutionDetailPage() {
  const { databaseId = '', executionId = '' } = useParams()
  const base = `/databases/${databaseId}/migrations`
  const execution = useQuery({
    queryKey: ['migration-execution', databaseId, executionId],
    queryFn: ({ signal }) => getExecution(databaseId, executionId, signal),
  })
  const attempt = execution.data

  return (
    <section className="execution-detail">
      <div className="page-header">
        <div className="page-header__text">
          <Link className="migration-back" to={base}>Riwayat migration</Link>
          <h1>{attempt ? `${executionActionText[attempt.action]} v${attempt.version}` : 'Detail execution'}</h1>
          {attempt?.migrationName ? <p className="muted">{attempt.migrationName}</p> : null}
        </div>
        {attempt ? <Link className="button button--secondary" to={`${base}/${attempt.migrationId}`}>Buka migration</Link> : null}
      </div>
      {execution.isPending ? <StatusMessage tone="loading">Memuat detail execution...</StatusMessage> : null}
      {execution.isError ? <StatusMessage tone="error">{describeError(execution.error)}</StatusMessage> : null}
      {attempt ? (
        <>
          <div className="panel">
            <dl className="execution-detail__meta">
              <div><dt>Status attempt</dt><dd>{executionStatusText[attempt.status] ?? attempt.status}</dd></div>
              <div><dt>Revisi</dt><dd>{attempt.revision}</dd></div>
              <div><dt>Oleh</dt><dd>{attempt.actorUsername}</dd></div>
              <div><dt>Waktu</dt><dd>{displayTime(attempt.requestedAt)}</dd></div>
              <div><dt>Execution ID</dt><dd><code>{attempt.executionId}</code></dd></div>
              {attempt.relatedExecutionId ? (
                <div>
                  <dt>Execution terkait</dt>
                  <dd><Link to={`${base}/executions/${attempt.relatedExecutionId}`}><code>{attempt.relatedExecutionId}</code></Link></dd>
                </div>
              ) : null}
            </dl>
          </div>
          {attempt.errorMessage ? <StatusMessage tone="error">{attempt.errorMessage}</StatusMessage> : null}
          <div className="panel">
            <h2>{attempt.action === 'rollback' ? 'Snapshot Down SQL' : 'Snapshot Up SQL'}</h2>
            <pre className="migration-sql-preview">{attempt.sqlSnapshot}</pre>
          </div>
          {attempt.events?.length ? (
            <details className="panel">
              <summary>Peristiwa execution ({attempt.events.length})</summary>
              <ol className="execution-detail__events">
                {attempt.events.map((event) => (
                  <li key={event.id}>
                    <strong>{eventText[event.event] ?? event.event}</strong>
                    {event.batchNumber != null ? ` · Batch ${event.batchNumber}` : ''}
                    {event.rowsAffected != null ? ` · ${event.rowsAffected} row` : ''}
                    <span className="muted"> · {displayTime(event.occurredAt)}</span>
                    {event.errorMessage ? <p>{event.errorMessage}</p> : null}
                    {event.querySql && event.querySql !== attempt.sqlSnapshot ? <pre className="migration-sql-preview">{event.querySql}</pre> : null}
                    {event.queryParameters?.length ? <p className="muted">Parameter: {event.queryParameters.join(', ')}</p> : null}
                  </li>
                ))}
              </ol>
            </details>
          ) : null}
        </>
      ) : null}
    </section>
  )
}
