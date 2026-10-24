// Latar hutan senja untuk panel kiri login: bukit, pohon pinus, padang rumput,
// dan kunang-kunang. Seluruhnya dekoratif (aria-hidden di induknya). Pohon dan
// rumput bergeser ke kiri (parallax) sehingga Senja terlihat berjalan.

interface Pine {
  x: number
  h: number
  w: number
}

// Pohon pinus tiga tingkat; sudutnya dibulatkan lewat stroke agar terlihat lucu.
function pinePath({ x, h, w }: Pine, baseY: number) {
  const top = baseY - h
  const tier = (offset: number, bottom: number, half: number) =>
    `M${x} ${top + h * offset} L${x - w * half} ${top + h * bottom} L${x + w * half} ${top + h * bottom}Z`
  return [
    tier(0, 0.36, 0.34),
    tier(0.18, 0.62, 0.44),
    tier(0.4, 0.88, 0.52),
    `M${x - w * 0.07} ${top + h * 0.86}h${w * 0.14}v${h * 0.14}h${-w * 0.14}Z`,
  ].join(' ')
}

// Satu ubin hutan 1200x400 yang diulang ke samping; pohon tidak melewati tepi
// ubin agar sambungannya tidak terlihat saat bergeser.
const backPines: Pine[] = [
  { x: 80, h: 250, w: 120 },
  { x: 230, h: 300, w: 140 },
  { x: 400, h: 220, w: 110 },
  { x: 560, h: 280, w: 130 },
  { x: 720, h: 200, w: 100 },
  { x: 880, h: 270, w: 125 },
  { x: 1020, h: 320, w: 150 },
  { x: 1130, h: 230, w: 110 },
]

const frontPines: Pine[] = [
  { x: 150, h: 220, w: 120 },
  { x: 330, h: 170, w: 95 },
  { x: 480, h: 260, w: 135 },
  { x: 650, h: 150, w: 85 },
  { x: 800, h: 230, w: 125 },
  { x: 960, h: 180, w: 100 },
  { x: 1100, h: 250, w: 130 },
]

function treeGroup(pines: Pine[], color: string, opacity = 1) {
  const paths = pines.map((pine) => `<path d="${pinePath(pine, 400)}"/>`).join('')
  return `<g fill="${color}" stroke="${color}" stroke-linejoin="round" stroke-width="10" opacity="${opacity}">${paths}</g>`
}

const treesTile = `url("data:image/svg+xml,${encodeURIComponent(
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 400" width="1200" height="400">${treeGroup(
    backPines,
    '#a9b979',
    0.85,
  )}${treeGroup(frontPines, '#6f9a5c')}</svg>`,
)}")`

const fireflies = [
  { left: '14%', top: '58%', delay: '0s' },
  { left: '24%', top: '44%', delay: '1.1s' },
  { left: '33%', top: '66%', delay: '2.3s' },
  { left: '68%', top: '50%', delay: '0.6s' },
  { left: '78%', top: '62%', delay: '1.8s' },
  { left: '86%', top: '42%', delay: '2.9s' },
  { left: '9%', top: '36%', delay: '3.4s' },
  { left: '91%', top: '70%', delay: '1.4s' },
]

export function LoginForest() {
  return (
    <div className="forest">
      <span className="forest__sun" />

      <svg className="forest__hills" preserveAspectRatio="xMidYMax slice" viewBox="0 0 1200 300">
        <path
          d="M0 170 C 200 80 380 135 560 105 C 760 70 950 150 1200 95 L1200 300 L0 300Z"
          fill="#ffd3b8"
          opacity="0.7"
        />
        <path
          d="M0 225 C 180 165 340 205 520 185 C 720 160 900 225 1200 175 L1200 300 L0 300Z"
          fill="#f7a183"
          opacity="0.75"
        />
      </svg>

      <div className="forest__trees" style={{ backgroundImage: treesTile }} />

      <svg className="forest__ground" preserveAspectRatio="none" viewBox="0 0 1200 200">
        <defs>
          <linearGradient id="forest-ground" x1="0" x2="0" y1="0" y2="1">
            <stop offset="0" stopColor="#a6cf63" />
            <stop offset="1" stopColor="#6c9c47" />
          </linearGradient>
        </defs>
        <path
          d="M0 38 C 150 10 300 30 450 22 C 620 12 760 40 900 28 C 1020 18 1120 30 1200 22 L1200 200 L0 200Z"
          fill="url(#forest-ground)"
        />
        <path d="M380 46 C 500 14 700 14 820 46 C 700 40 500 40 380 46Z" fill="#b8da74" />
      </svg>

      <span className="forest__grass" />

      {fireflies.map((firefly) => (
        <span
          className="forest__firefly"
          key={`${firefly.left}-${firefly.top}`}
          style={{ left: firefly.left, top: firefly.top, animationDelay: firefly.delay }}
        />
      ))}
    </div>
  )
}
