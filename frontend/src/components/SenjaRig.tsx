// ============================================================================
// FILE INI UNTUK APA (SenjaRig.tsx)
// ----------------------------------------------------------------------------
// Senja versi rig (SVG berlapis) yang bergerak: kepala, telinga, mata, mulut,
// lengan, kaki, ekor, syal, dan lentera bisa bergerak sendiri-sendiri.
// Animasinya dibuat di HyperFrames (lihat assets/brand/senja-rig/) dan
// diputar oleh SenjaPlayer.
//
// Pemakaian:
//   <SenjaRig loop="idle" intro="stretch" gesture="wave" gestureTrigger={heroRef} />
//   - loop            animasi yang terus berulang
//   - intro           diputar sekali saat muncul, lalu lanjut ke loop
//   - gesture         diputar sekali saat pointer masuk ke gestureTrigger
//                     (atau ke Senja sendiri bila gestureTrigger kosong)
//   - outfit          kostum sesuai waktu (default 'sore' = membawa lentera):
//                     pagi = mug kopi, siang = topi jerami + es kopi,
//                     sore = lentera, malam = topi tidur + piyama + bantal
// Dekoratif: aria-hidden, sama seperti <img alt=""> yang digantikannya.
// ============================================================================

import { useEffect, useId, useMemo, useRef, type RefObject } from 'react'

import { SENJA_SVG, type SenjaClipName } from '@/assets/brand/senja-rig/web/senja-data'
import { SenjaPlayer } from '@/components/senja-player'
import '@/components/senja-rig.css'

export type SenjaOutfit = 'pagi' | 'siang' | 'sore' | 'malam'

interface SenjaRigProps {
  loop: SenjaClipName
  intro?: SenjaClipName
  gesture?: SenjaClipName
  gestureTrigger?: RefObject<HTMLElement | null>
  outfit?: SenjaOutfit
  className?: string
}

export function SenjaRig({ loop, intro, gesture, gestureTrigger, outfit = 'sore', className }: SenjaRigProps) {
  const hostRef = useRef<HTMLSpanElement>(null)
  const playerRef = useRef<SenjaPlayer | null>(null)
  // useId berisi ":" -> dibersihkan agar aman dipakai sebagai id SVG dan url(#...).
  const prefix = `senja${useId().replace(/[^a-zA-Z0-9]/g, '')}`
  const markup = useMemo(() => SENJA_SVG.replaceAll('__SENJA__', prefix), [prefix])

  useEffect(() => {
    const svg = hostRef.current?.querySelector('svg')
    if (!svg) return
    const player = new SenjaPlayer(svg, prefix)
    playerRef.current = player
    player.play(loop, intro)
    return () => {
      player.destroy()
      playerRef.current = null
    }
  }, [prefix, loop, intro])

  useEffect(() => {
    if (!gesture) return
    const target = gestureTrigger?.current ?? hostRef.current
    if (!target) return
    const onEnter = () => playerRef.current?.gesture(gesture)
    target.addEventListener('pointerenter', onEnter)
    return () => target.removeEventListener('pointerenter', onEnter)
  }, [gesture, gestureTrigger])

  return (
    <span
      aria-hidden="true"
      className={className ? `senja-rig ${className}` : 'senja-rig'}
      dangerouslySetInnerHTML={{ __html: markup }}
      data-outfit={outfit}
      data-senja-loop={loop}
      ref={hostRef}
    />
  )
}
