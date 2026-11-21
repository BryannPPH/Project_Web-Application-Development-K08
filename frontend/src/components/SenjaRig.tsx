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
