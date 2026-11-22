import { useMemo } from 'react'

import caveDark from '@/assets/brand/cave-scene.svg?raw'
import caveLight from '@/assets/brand/cave-scene-light.svg?raw'
import { useTheme } from '@/lib/theme'
import '@/components/cave-scene.css'

// Potongan gambar: lebar (banner dashboard), pita atas (halaman dalam: stalaktit,
// lengkung, lentera gantung), dan potret (panel login: dua lentera di tengah).
const crops = {
  wide: { aspect: 'xMaxYMax slice' },
  band: { aspect: 'xMaxYMin slice' },
  portrait: { aspect: 'xMidYMax meet', viewBox: '1110 0 780 640' },
} as const

export type CaveVariant = keyof typeof crops

// Latar gua dengan lentera (Emberden): gua gelap untuk mode gelap, gua terang
// berwarna lavender untuk mode terang. SVG inline supaya api dan pendar lentera
// bisa dianimasikan lewat CSS; hiasan murni, jadi disembunyikan dari pembaca
// layar. Aset milik sendiri, bukan input pengguna.
export function CaveScene({ className, variant = 'wide' }: { className?: string; variant?: CaveVariant }) {
  const { theme } = useTheme()
  const markup = useMemo(() => {
    const crop = crops[variant]
    const source = theme === 'dark' ? caveDark : caveLight
    const cropped = source.replace('preserveAspectRatio="xMaxYMax slice"', `preserveAspectRatio="${crop.aspect}"`)
    return 'viewBox' in crop ? cropped.replace('viewBox="0 0 2400 640"', `viewBox="${crop.viewBox}"`) : cropped
  }, [theme, variant])

  return (
    <span
      aria-hidden="true"
      className={className ? `cave-scene ${className}` : 'cave-scene'}
      dangerouslySetInnerHTML={{ __html: markup }}
    />
  )
}
