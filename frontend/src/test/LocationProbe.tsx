import { useLocation } from 'react-router-dom'

// LocationProbe menampilkan URL aktif agar test dapat memeriksa navigasi.
export function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{`${location.pathname}${location.search}`}</output>
}
