import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'

import { describeError } from '@/components/error-message'
import { Icon } from '@/components/Icon'
import { StatusMessage } from '@/components/StatusMessage'
import { explainMigrationSQL } from '@/modules/migrations/api'
import type { ExplainResult, PlanNode, PlanSummary, StatementPlan } from '@/modules/migrations/types'

const numberFormat = new Intl.NumberFormat('id-ID', { maximumFractionDigits: 2 })

function formatNumber(value: number | undefined) {
  return value === undefined ? '–' : numberFormat.format(value)
}

function formatMs(value: number | undefined) {
  return value === undefined ? '–' : `${numberFormat.format(value)} ms`
}

interface ExplainPanelProps {
  databaseId: string
  sql: string
}

// Query execution plan analysis: EXPLAIN / EXPLAIN ANALYZE terhadap SQL
// migration sebelum dieksekusi. Backend selalu me-rollback analisis.
export function ExplainPanel({ databaseId, sql }: ExplainPanelProps) {
  const [analyzedSql, setAnalyzedSql] = useState<string | null>(null)
  const explain = useMutation({
    mutationFn: ({ analyze, text }: { analyze: boolean; text: string }) => explainMigrationSQL(databaseId, text, analyze),
    onSuccess: (_, variables) => setAnalyzedSql(variables.text),
  })
  const empty = sql.trim() === ''
  const stale = explain.data !== undefined && analyzedSql !== sql

  return (
    <section aria-labelledby="explain-title" className="panel explain-panel">
      <div className="panel__header">
        <div>
          <h3 id="explain-title">Analisis execution plan</h3>
          <p className="muted">
            Jalankan EXPLAIN untuk estimasi, atau EXPLAIN ANALYZE untuk waktu dan jumlah row aktual.
          </p>
        </div>
        <div className="inline-actions">
          <button
            className="button button--secondary"
            disabled={empty || explain.isPending}
            onClick={() => explain.mutate({ analyze: false, text: sql })}
            type="button"
          >
            <Icon name="search" size={16} />
            EXPLAIN
          </button>
          <button
            className="button button--primary"
            disabled={empty || explain.isPending}
            onClick={() => explain.mutate({ analyze: true, text: sql })}
            type="button"
          >
            <Icon className={explain.isPending ? 'spinner' : undefined} name={explain.isPending ? 'loader' : 'zap'} size={16} />
            {explain.isPending ? 'Menganalisis…' : 'EXPLAIN ANALYZE'}
          </button>
        </div>
      </div>

      <p className="explain-safety">
        <Icon name="shield" size={14} />
        Analisis berjalan di dalam transaksi yang selalu di-rollback, sehingga tidak ada data yang berubah.
      </p>

      {empty ? <StatusMessage tone="empty">Tulis SQL migration terlebih dahulu untuk dianalisis.</StatusMessage> : null}
      {explain.isError ? (
        <StatusMessage title="Analisis gagal" tone="error">
          {describeError(explain.error)}
        </StatusMessage>
      ) : null}
      {stale ? (
        <StatusMessage tone="warning">SQL sudah berubah sejak analisis terakhir. Jalankan ulang untuk hasil terbaru.</StatusMessage>
      ) : null}
      {explain.data ? <ExplainResults result={explain.data} /> : null}
    </section>
  )
}

function ExplainResults({ result }: { result: ExplainResult }) {
  return (
    <div className="explain-results">
      <p className="muted">
        {result.analyze ? 'EXPLAIN ANALYZE' : 'EXPLAIN'} · {result.engine === 'mysql' ? 'MySQL' : 'PostgreSQL'} ·{' '}
        {result.statements.length} statement
      </p>
      {result.statements.map((statement) => (
        <StatementResult key={statement.index} statement={statement} />
      ))}
    </div>
  )
}

const kindLabels: Record<StatementPlan['kind'], { label: string; className: string }> = {
  plan: { label: 'Plan', className: 'badge badge--info' },
  executed: { label: 'DDL dijalankan sementara', className: 'badge' },
  error: { label: 'Gagal', className: 'badge badge--danger' },
}

function StatementResult({ statement }: { statement: StatementPlan }) {
  const kind = kindLabels[statement.kind]
  return (
    <article className="explain-statement" aria-label={`Statement ${statement.index}`}>
      <header className="explain-statement__header">
        <span className="explain-statement__number">#{statement.index}</span>
        <code className="explain-statement__sql">{statement.sql}</code>
        <span className={kind.className}>{kind.label}</span>
      </header>

      {statement.error ? (
        <p className="form-error explain-statement__error" role="alert">
          <Icon name="error" size={16} />
          {statement.error}
        </p>
      ) : null}
      {statement.note ? <p className="muted explain-statement__note">{statement.note}</p> : null}
      {statement.summary ? <SummaryView summary={statement.summary} /> : null}
      {statement.plan ? (
        <div className="plan-tree" role="tree" aria-label={`Execution plan statement ${statement.index}`}>
          <PlanTreeNode node={statement.plan} />
        </div>
      ) : null}
      {statement.raw ? (
        <details className="explain-raw">
          <summary>Output mentah</summary>
          <pre>{statement.raw}</pre>
        </details>
      ) : null}
    </article>
  )
}

function SummaryView({ summary }: { summary: PlanSummary }) {
  const metrics = [
    { label: 'Estimasi cost', value: formatNumber(summary.totalCost) },
    { label: 'Estimasi row', value: formatNumber(summary.estimatedRows) },
    { label: 'Row aktual', value: formatNumber(summary.actualRows) },
    { label: 'Planning', value: formatMs(summary.planningMs) },
    { label: 'Eksekusi', value: formatMs(summary.executionMs) },
  ]
  return (
    <>
      <dl className="explain-metrics">
        {metrics.map((metric) => (
          <div key={metric.label}>
            <dt>{metric.label}</dt>
            <dd>{metric.value}</dd>
          </div>
        ))}
      </dl>
      <div className="explain-tags">
        <TagGroup empty="—" icon="table" items={summary.scanTypes} label="Jenis scan" />
        <TagGroup empty="Tanpa index" icon="key" items={summary.indexes} label="Index" />
        <TagGroup empty="Tanpa join" icon="link" items={summary.joinTypes} label="Join" />
      </div>
      {summary.warnings.map((warning) => (
        <StatusMessage key={warning} tone="warning">
          {warning}
        </StatusMessage>
      ))}
    </>
  )
}

function TagGroup({ label, items, empty, icon }: { label: string; items: string[]; empty: string; icon: 'table' | 'key' | 'link' }) {
  return (
    <div className="explain-tags__group">
      <span className="explain-tags__label">
        <Icon name={icon} size={13} />
        {label}
      </span>
      {items.length === 0 ? (
        <span className="badge">{empty}</span>
      ) : (
        items.map((item) => (
          <span className="badge badge--brand" key={item}>
            {item}
          </span>
        ))
      )}
    </div>
  )
}

function PlanTreeNode({ node }: { node: PlanNode }) {
  const actual = node.actualTotalMs !== undefined
  return (
    <div className="plan-node" role="treeitem" aria-expanded={node.children?.length ? true : undefined}>
      <div className="plan-node__line">
        <span className="plan-node__type">{node.nodeType}</span>
        {node.relation ? (
          <span className="plan-node__relation">
            pada <code>{node.relation}</code>
            {node.alias ? <span className="muted"> ({node.alias})</span> : null}
          </span>
        ) : null}
        {node.index ? (
          <span className="badge badge--brand">
            <Icon name="key" size={11} />
            {node.index}
          </span>
        ) : null}
        <span className="plan-node__metrics">
          {node.totalCost !== undefined ? <span>cost {formatNumber(node.totalCost)}</span> : null}
          {node.planRows !== undefined ? <span>~{formatNumber(node.planRows)} row</span> : null}
          {actual ? (
            <span className="plan-node__actual">
              {formatMs(node.actualTotalMs)} · {formatNumber(node.actualRows)} row
              {node.actualLoops !== undefined && node.actualLoops > 1 ? ` × ${formatNumber(node.actualLoops)} loop` : ''}
            </span>
          ) : null}
        </span>
      </div>
      {node.condition || node.filter ? (
        <code className="plan-node__condition">{node.condition ?? node.filter}</code>
      ) : null}
      {node.children?.length ? (
        <div className="plan-node__children" role="group">
          {node.children.map((child, index) => (
            <PlanTreeNode key={index} node={child} />
          ))}
        </div>
      ) : null}
    </div>
  )
}
