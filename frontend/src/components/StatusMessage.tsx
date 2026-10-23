import type { ReactNode } from 'react'

import '@/components/platform.css'

interface StatusMessageProps {
  tone?: 'info' | 'error' | 'empty'
  title?: string
  children?: ReactNode
  actionLabel?: string
  onAction?: () => void
}

// Kotak status generik untuk loading, error, dan empty state.
export function StatusMessage({
  tone = 'info',
  title,
  children,
  actionLabel,
  onAction,
}: StatusMessageProps) {
  return (
    <div
      className={`status-message status-message--${tone}`}
      role={tone === 'error' ? 'alert' : 'status'}
    >
      {title ? <strong>{title}</strong> : null}
      {children ? <div>{children}</div> : null}
      {actionLabel && onAction ? (
        <button className="button button--secondary" onClick={onAction} type="button">
          {actionLabel}
        </button>
      ) : null}
    </div>
  )
}
