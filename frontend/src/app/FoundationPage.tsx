export function FoundationPage() {
  return (
    <main className="foundation">
      <section className="foundation__card" aria-labelledby="foundation-title">
        <p className="foundation__eyebrow">IF3110 · Milestone 1</p>
        <h1 id="foundation-title">Database Management Platform</h1>
        <p className="foundation__description">
          The frontend foundation is ready. Product features will be added as
          isolated modules.
        </p>
        <div className="foundation__status" role="status">
          <span className="foundation__status-dot" aria-hidden="true" />
          React application initialized
        </div>
      </section>
    </main>
  )
}

