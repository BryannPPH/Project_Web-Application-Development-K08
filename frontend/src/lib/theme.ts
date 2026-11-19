// ============================================================================
// FILE INI UNTUK APA (theme.ts)
// ----------------------------------------------------------------------------
// Mode terang / gelap. Warna kedua mode ada di styles/global.css: atribut
// <html data-theme="dark"> mengganti seluruh token warna sekaligus.
//
//   - Pilihan user disimpan di localStorage ("dbm-theme").
//   - Belum pernah memilih -> ikut pengaturan sistem (prefers-color-scheme),
//     termasuk bila pengaturan sistem berubah saat aplikasi terbuka.
//   - index.html memasang tema sebelum halaman pertama kali digambar, jadi
//     tidak ada kilatan putih saat membuka aplikasi dalam mode gelap.
// ============================================================================

import { useCallback, useSyncExternalStore } from 'react'

export type Theme = 'light' | 'dark'

const STORAGE_KEY = 'dbm-theme'
const listeners = new Set<() => void>()

function systemTheme(): Theme {
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light'
}

function storedTheme(): Theme | null {
  try {
    const value = window.localStorage.getItem(STORAGE_KEY)
    return value === 'light' || value === 'dark' ? value : null
  } catch {
    return null
  }
}

function currentTheme(): Theme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
}

function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  const query = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-color-scheme: dark)') : null
  const onSystemChange = () => {
    if (!storedTheme()) applyTheme(systemTheme())
  }
  query?.addEventListener('change', onSystemChange)
  return () => {
    listeners.delete(listener)
    query?.removeEventListener('change', onSystemChange)
  }
}

/** Pasang tema awal (dipanggil sekali di main.tsx, cadangan bila skrip index.html gagal). */
export function initTheme() {
  applyTheme(storedTheme() ?? systemTheme())
}

export function useTheme() {
  const theme = useSyncExternalStore(subscribe, currentTheme, () => 'light' as Theme)
  const toggle = useCallback(() => {
    const next: Theme = currentTheme() === 'dark' ? 'light' : 'dark'
    try {
      window.localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // Penyimpanan diblokir (mode privat): tema tetap berganti untuk sesi ini.
    }
    applyTheme(next)
  }, [])
  return { theme, toggle }
}
