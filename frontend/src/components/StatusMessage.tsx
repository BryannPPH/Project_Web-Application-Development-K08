import type { ReactNode } from 'react'

import { senja, type SenjaPose } from '@/components/brand'
import { Icon, type IconName } from '@/components/Icon'
import '@/components/platform.css'

type Tone = 'info' | 'loading' | 'success' | 'warning' | 'error' | 'empty'

interface StatusMessageProps {
  tone?: Tone
  title?: string
  children?: ReactNode
  actionLabel?: string
  onAction?: () => void
  illustration?: SenjaPose
}

const toneIcons: Record<Tone, IconName> = {
  info: 'info',
  loading: 'loader',
  success: 'check',
  warning: 'alert',
  error: 'error',
  empty: 'info',
}

// Kotak status generik untuk loading, error, dan empty state. Dengan
// `illustration`, kotak berubah menjadi empty state lengkap bersama Senja.
export function StatusMessage({
  tone = 'info',
  title,
  children,
  actionLabel,
  onAction,
  illustration,
}: StatusMessageProps) {
  const role = tone === 'error' ? 'alert' : 'status'
  const action =
    actionLabel && onAction ? (
      <button className="button button--secondary button--small" onClick={onAction} type="button">
        <Icon name="refresh" size={14} />
        {actionLabel}
      </button>
    ) : null

  if (illustration) {
    return (
      <div className={`empty-state empty-state--${tone}`} role={role}>
        <img alt="" className="empty-state__art" src={senja[illustration]} />
        {title ? <strong className="empty-state__title">{title}</strong> : null}
        {children ? <div className="empty-state__text">{children}</div> : null}
        {action}
      </div>
    )
  }

  return (
    <div className={`status-message status-message--${tone}`} role={role}>
      <Icon
        className={tone === 'loading' ? 'status-message__icon spinner' : 'status-message__icon'}
        name={toneIcons[tone]}
        size={16}
      />
      <div className="status-message__body">
        {title ? <strong>{title}</strong> : null}
        {children ? <div>{children}</div> : null}
        {action}
      </div>
    </div>
  )
}
