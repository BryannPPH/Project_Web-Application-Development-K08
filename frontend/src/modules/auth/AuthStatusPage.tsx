import { senja } from '@/components/brand'
import { Icon } from '@/components/Icon'
import '@/modules/auth/auth.css'

interface AuthStatusPageProps {
  actionLabel?: string
  message: string
  onAction?: () => void
  title: string
}

export function AuthStatusPage({
  actionLabel,
  message,
  onAction,
  title,
}: AuthStatusPageProps) {
  const failed = Boolean(actionLabel && onAction)
  return (
    <main className="auth-status">
      <section className="auth-status__card" aria-live="polite">
        <img alt="" className="auth-status__art" src={failed ? senja.unplugged : senja.lantern} />
        <h1>{title}</h1>
        <p>{message}</p>
        {actionLabel && onAction ? (
          <button className="button button--primary" onClick={onAction} type="button">
            <Icon name="refresh" size={16} />
            {actionLabel}
          </button>
        ) : null}
      </section>
    </main>
  )
}
