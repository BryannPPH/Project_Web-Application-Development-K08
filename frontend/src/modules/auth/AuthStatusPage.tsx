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
  return (
    <main className="auth-status">
      <section className="auth-status__card" aria-live="polite">
        <h1>{title}</h1>
        <p>{message}</p>
        {actionLabel && onAction ? (
          <button className="button button--primary" onClick={onAction} type="button">
            {actionLabel}
          </button>
        ) : null}
      </section>
    </main>
  )
}
