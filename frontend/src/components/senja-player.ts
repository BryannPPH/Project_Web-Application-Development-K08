// ============================================================================
// FILE INI UNTUK APA (senja-player.ts)
// ----------------------------------------------------------------------------
// Pemutar animasi rig Senja di website, tanpa library animasi.
//
// Gerakannya TIDAK ditulis di sini. Animasi dibuat di HyperFrames
// (assets/brand/senja-rig/scripts/senja-motion.js), lalu build-rig.mjs
// merekamnya menjadi data keyframe (senja-data.ts): untuk tiap layer rig,
// "mulai detik ke-s, selama d detik, ubah properti ke nilai v dengan easing e".
// File ini hanya membaca data itu tiap frame dan menulis atribut `transform`
// / `opacity` ke grup SVG yang sesuai. Jadi gerakan di web = gerakan di
// pratinjau HyperFrames.
//
// Perilaku:
//   - intro (sekali) lalu loop (berulang); gesture (mis. melambai saat banner
//     disorot) diputar sekali lalu kembali ke loop.
//   - Pergantian klip dibaurkan 250ms agar pose tidak meloncat.
//   - prefers-reduced-motion: pose diam (frame pertama loop), tanpa rAF.
//   - Di luar layar (IntersectionObserver): tidak menggambar apa pun.
// ============================================================================

import {
  SENJA_CLIPS,
  SENJA_ORIGINS,
  type SenjaClip,
  type SenjaClipName,
} from '@/assets/brand/senja-rig/web/senja-data'

type Ease = (p: number) => number

const TRANSFORM_PROPS = ['x', 'y', 'rotation', 'skewX', 'scaleX', 'scaleY'] as const
const DEFAULTS: Record<string, number> = { x: 0, y: 0, rotation: 0, skewX: 0, scaleX: 1, scaleY: 1 }
const BLEND_SECONDS = 0.25

// Kurva easing yang dipakai senja-motion.js, dengan rumus yang sama seperti GSAP.
function powerEase(kind: string, power: number): Ease {
  const n = power + 1
  if (kind === 'in') return (p) => p ** n
  if (kind === 'out') return (p) => 1 - (1 - p) ** n
  return (p) => (p < 0.5 ? (2 * p) ** n / 2 : 1 - (2 * (1 - p)) ** n / 2)
}

function easeFor(name: string): Ease {
  if (name === 'none') return (p) => p
  const back = /^back\.out(?:\(([\d.]+)\))?$/.exec(name)
  if (back) {
    const s = back[1] ? Number(back[1]) : 1.70158
    return (p) => {
      const q = p - 1
      return q * q * ((s + 1) * q + s) + 1
    }
  }
  const [family, kind = 'out'] = name.split('.')
  if (family === 'sine') {
    if (kind === 'in') return (p) => 1 - Math.cos((p * Math.PI) / 2)
    if (kind === 'out') return (p) => Math.sin((p * Math.PI) / 2)
    return (p) => -(Math.cos(Math.PI * p) - 1) / 2
  }
  const power = /^power(\d)$/.exec(family ?? '')
  if (power) return powerEase(kind, Number(power[1]))
  return (p) => p
}

interface Segment {
  start: number
  end: number
  to: number
  ease: Ease
}

// layer id -> properti -> segmen berurutan
type CompiledClip = Map<string, Map<string, Segment[]>>

const compiled = new Map<SenjaClipName, CompiledClip>()

function compile(name: SenjaClipName): CompiledClip {
  const cached = compiled.get(name)
  if (cached) return cached
  const clip: SenjaClip = SENJA_CLIPS[name]
  const tracks: CompiledClip = new Map()
  // Urutan stabil per waktu mulai: dua "set" di detik yang sama dijalankan sesuai urutan tulis.
  const ops = clip.ops.map((op, index) => ({ op, index })).sort((a, b) => a.op[0] - b.op[0] || a.index - b.index)
  for (const { op } of ops) {
    const [start, duration, id, props, ease] = op
    let layer = tracks.get(id)
    if (!layer) {
      layer = new Map()
      tracks.set(id, layer)
    }
    for (const [prop, to] of Object.entries(props)) {
      let segments = layer.get(prop)
      if (!segments) {
        segments = []
        layer.set(prop, segments)
      }
      segments.push({ start, end: start + duration, to, ease: easeFor(ease) })
    }
  }
  compiled.set(name, tracks)
  return tracks
}

function sample(segments: Segment[] | undefined, t: number, initial: number) {
  let value = initial
  if (!segments) return value
  for (const segment of segments) {
    if (t < segment.start) break
    if (t >= segment.end) {
      value = segment.to
      continue
    }
    const p = (t - segment.start) / (segment.end - segment.start)
    return value + (segment.to - value) * segment.ease(p)
  }
  return value
}

// Semua (layer, properti) yang pernah disentuh klip mana pun, supaya properti
// yang tidak dipakai klip aktif tetap kembali ke nilai default.
const allProps = new Map<string, Set<string>>()
for (const clip of Object.values(SENJA_CLIPS)) {
  for (const [, , id, props] of clip.ops) {
    const set = allProps.get(id) ?? new Set<string>()
    Object.keys(props).forEach((prop) => set.add(prop))
    allProps.set(id, set)
  }
}

type Pose = Map<string, Record<string, number>>

interface Layer {
  id: string
  element: SVGElement
  props: string[]
  hasTransform: boolean
  initialOpacity: number
}

interface Playing {
  name: SenjaClipName
  startedAt: number
  once: boolean
}

export class SenjaPlayer {
  private readonly layers: Layer[]
  private loopName: SenjaClipName = 'idle'
  private current: Playing | null = null
  private blendFrom: Pose | null = null
  private blendStart = 0
  private lastPose: Pose = new Map()
  private frame = 0
  private visible = true
  private reduced = false
  private readonly motionQuery: MediaQueryList | null
  private readonly observer: IntersectionObserver | null

  constructor(svg: SVGSVGElement, prefix: string) {
    this.layers = []
    for (const [id, props] of allProps) {
      const element = svg.querySelector<SVGElement>(`[id="${prefix}-${id}"]`)
      if (!element) continue
      const list = [...props]
      this.layers.push({
        id,
        element,
        props: list,
        hasTransform: list.some((prop) => (TRANSFORM_PROPS as readonly string[]).includes(prop)),
        initialOpacity: Number(element.getAttribute('opacity') ?? 1),
      })
    }

    this.motionQuery = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-reduced-motion: reduce)') : null
    this.reduced = this.motionQuery?.matches ?? false
    this.motionQuery?.addEventListener('change', this.onMotionChange)

    this.observer =
      typeof IntersectionObserver === 'function'
        ? new IntersectionObserver(([entry]) => {
            this.visible = entry?.isIntersecting ?? true
            if (this.visible) this.schedule()
          })
        : null
    this.observer?.observe(svg)
  }

  /** Putar `intro` sekali (jika ada), lalu `loop` berulang. */
  play(loop: SenjaClipName, intro?: SenjaClipName) {
    this.loopName = loop
    this.switchTo(intro ?? loop, Boolean(intro), false)
  }

  /** Putar klip sekali lalu kembali ke loop; diabaikan selama intro/gesture masih jalan. */
  gesture(name: SenjaClipName) {
    if (this.reduced || this.current?.once) return
    this.switchTo(name, true, true)
  }

  destroy() {
    cancelAnimationFrame(this.frame)
    this.observer?.disconnect()
    this.motionQuery?.removeEventListener('change', this.onMotionChange)
  }

  private onMotionChange = (event: MediaQueryListEvent) => {
    this.reduced = event.matches
    this.play(this.loopName)
  }

  private switchTo(name: SenjaClipName, once: boolean, blend: boolean) {
    const now = performance.now()
    this.blendFrom = blend && this.lastPose.size > 0 ? this.lastPose : null
    this.blendStart = now
    this.current = { name, startedAt: now, once }
    if (this.reduced) {
      cancelAnimationFrame(this.frame)
      this.render(this.loopName, 0, now)
      return
    }
    this.schedule()
  }

  private schedule() {
    if (this.reduced || typeof requestAnimationFrame !== 'function') {
      this.render(this.loopName, 0, performance.now())
      return
    }
    cancelAnimationFrame(this.frame)
    this.frame = requestAnimationFrame(this.tick)
  }

  private tick = (now: number) => {
    const playing = this.current
    if (!playing || !this.visible) return
    const length = SENJA_CLIPS[playing.name].length
    let t = (now - playing.startedAt) / 1000
    if (playing.once && t >= length) {
      // Intro/gesture selesai -> lanjut ke loop. Klip berakhir di pose netral,
      // dan loop dimulai dari pose netral, jadi tidak perlu dibaurkan.
      this.current = { name: this.loopName, startedAt: now, once: false }
      this.blendFrom = null
      this.render(this.loopName, 0, now)
    } else {
      if (!playing.once) t %= length
      this.render(playing.name, t, now)
    }
    this.frame = requestAnimationFrame(this.tick)
  }

  private render(name: SenjaClipName, t: number, now: number) {
    const clip = compile(name)
    const blendP = this.blendFrom ? Math.min((now - this.blendStart) / 1000 / BLEND_SECONDS, 1) : 1
    const blendEase = 1 - (1 - blendP) ** 3
    const pose: Pose = new Map()

    for (const layer of this.layers) {
      const tracks = clip.get(layer.id)
      const values: Record<string, number> = {}
      for (const prop of layer.props) {
        const initial = prop === 'opacity' ? layer.initialOpacity : (DEFAULTS[prop] ?? 0)
        let value = sample(tracks?.get(prop), t, initial)
        const from = this.blendFrom?.get(layer.id)?.[prop]
        // Hanya transform yang dibaurkan; ganti ekspresi (opacity) tetap instan seperti animasi 2D.
        if (from !== undefined && prop !== 'opacity' && blendP < 1) value = from + (value - from) * blendEase
        values[prop] = value
      }
      pose.set(layer.id, values)
      this.apply(layer, values)
    }
    if (blendP >= 1) this.blendFrom = null
    this.lastPose = pose
  }

  private apply(layer: Layer, v: Record<string, number>) {
    if (layer.hasTransform) {
      const [ox, oy] = SENJA_ORIGINS[layer.id] ?? [0, 0]
      const x = v.x ?? 0
      const y = v.y ?? 0
      const rotation = v.rotation ?? 0
      const skewX = v.skewX ?? 0
      const scaleX = v.scaleX ?? 1
      const scaleY = v.scaleY ?? 1
      // Urutan sama dengan GSAP: geser, lalu putar/miring/skala di sekitar pivot layer.
      layer.element.setAttribute(
        'transform',
        `translate(${x + ox} ${y + oy}) rotate(${rotation}) skewX(${skewX}) scale(${scaleX} ${scaleY}) translate(${-ox} ${-oy})`,
      )
    }
    if (v.opacity !== undefined) layer.element.style.opacity = String(v.opacity)
  }
}
