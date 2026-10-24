import { Link } from 'react-router-dom'

import { senja } from '@/components/brand'
import { Icon } from '@/components/Icon'

export function NotFoundPage() {
  return (
    <div className="error-page">
      <img alt="" className="error-page__art" src={senja.lostMap} />
      <h1>Halaman tidak ditemukan</h1>
      <p>Senja sudah membaca petanya, tapi halaman ini tidak ada. Mungkin tautannya sudah berubah.</p>
      <Link className="button button--primary" to="/">
        <Icon name="dashboard" size={16} />
        Kembali ke Dashboard
      </Link>
    </div>
  )
}
