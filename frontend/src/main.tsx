import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

// Stylesheet global dimuat lebih dulu agar CSS per-modul dapat menimpanya.
import '@/styles/global.css'
import '@/components/platform.css'
import { App } from '@/app/App'

const rootElement = document.getElementById('root')

if (!rootElement) {
  throw new Error('Root element was not found')
}

createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

