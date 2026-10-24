import logoMark from '@/assets/brand/logo-mark.png'
import logoMarkLight from '@/assets/brand/logo-mark-light.png'
import senjaEmptyBox from '@/assets/brand/senja-empty-box.png'
import senjaLantern from '@/assets/brand/senja-lantern.png'
import senjaLocked from '@/assets/brand/senja-locked.png'
import senjaLostMap from '@/assets/brand/senja-lost-map.png'
import senjaRewind from '@/assets/brand/senja-rewind.png'
import senjaSearching from '@/assets/brand/senja-searching.png'
import senjaSleeping from '@/assets/brand/senja-sleeping.png'
import senjaSuccess from '@/assets/brand/senja-success.png'
import senjaUnplugged from '@/assets/brand/senja-unplugged.png'
import senjaWaving from '@/assets/brand/senja-waving.png'

export { logoMark, logoMarkLight }

// Pose maskot Senja per konteks (ui-design-brief §4). Senja hanya dipakai di
// margin: login, empty state, error/forbidden, dan hasil migration.
export const senja = {
  waving: senjaWaving,
  emptyBox: senjaEmptyBox,
  searching: senjaSearching,
  locked: senjaLocked,
  lostMap: senjaLostMap,
  unplugged: senjaUnplugged,
  lantern: senjaLantern,
  success: senjaSuccess,
  rewind: senjaRewind,
  sleeping: senjaSleeping,
} as const

export type SenjaPose = keyof typeof senja
