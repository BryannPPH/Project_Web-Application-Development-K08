export type HintLevel = 'breaking' | 'caution'

export interface CompatibilityHint {
  level: HintLevel
  title: string
  detail: string
}

interface Rule extends CompatibilityHint {
  pattern: RegExp
}

// Urutan = urutan tampil. "breaking" = hampir pasti memengaruhi pengguna lain.
const rules: Rule[] = [
  {
    level: 'breaking',
    pattern: /\bdrop\s+table\b/i,
    title: 'Menghapus tabel',
    detail: 'Semua data dan query yang memakai tabel ini akan gagal.',
  },
  {
    level: 'breaking',
    pattern: /\bdrop\s+column\b|\balter\s+table\b[^;]*\bdrop\s+(?!constraint\b|index\b|default\b|not\s+null\b)\w+/i,
    title: 'Menghapus kolom',
    detail: 'Data kolom hilang; query, laporan, atau aplikasi yang membaca kolom ini akan error.',
  },
  {
    level: 'breaking',
    pattern: /\brename\s+(column\s+)?\w+\s+to\b|\brename\s+to\b/i,
    title: 'Mengganti nama tabel/kolom',
    detail: 'Query yang masih memakai nama lama akan gagal.',
  },
  {
    level: 'breaking',
    pattern: /\balter\s+column\b[^;]*\btype\b|\bmodify\s+column\b/i,
    title: 'Mengubah tipe kolom',
    detail: 'Nilai lama bisa gagal dikonversi, dan aplikasi bisa membaca tipe yang berbeda.',
  },
  {
    level: 'breaking',
    pattern: /\btruncate\b/i,
    title: 'Mengosongkan tabel (TRUNCATE)',
    detail: 'Semua baris dihapus tanpa bisa dikembalikan.',
  },
  {
    level: 'caution',
    pattern: /\bdelete\s+from\b(?![^;]*\bwhere\b)|\bupdate\s+\w[\w."]*\s+set\b(?![^;]*\bwhere\b)/i,
    title: 'UPDATE/DELETE tanpa WHERE',
    detail: 'Perintah ini mengubah SEMUA baris di tabel. Pastikan memang disengaja.',
  },
  {
    level: 'caution',
    pattern: /\badd\s+(column\s+)?\w+\s+[^,;]*\bnot\s+null\b(?![^,;]*\bdefault\b)/i,
    title: 'Kolom NOT NULL tanpa DEFAULT',
    detail: 'Gagal kalau tabel sudah berisi data, dan INSERT lama yang tidak mengisi kolom ini akan ditolak.',
  },
  {
    level: 'caution',
    pattern: /\bset\s+not\s+null\b|\badd\s+constraint\b|\bunique\b|\bforeign\s+key\b|\breferences\b/i,
    title: 'Menambah aturan (constraint)',
    detail: 'Data lama yang melanggar aturan baru membuat migration gagal, dan INSERT/UPDATE aplikasi bisa mulai ditolak.',
  },
  {
    level: 'caution',
    pattern: /\bdrop\s+(index|constraint|view|function|trigger)\b/i,
    title: 'Menghapus index, constraint, view, atau function',
    detail: 'Query bisa melambat (index) atau objek yang dipakai pengguna lain hilang.',
  },
]

/** Menghapus komentar SQL supaya contoh di komentar tidak memicu peringatan. */
function stripComments(sql: string) {
  return sql.replace(/\/\*[\s\S]*?\*\//g, ' ').replace(/--[^\n]*/g, ' ')
}

export function compatibilityHints(sql: string): CompatibilityHint[] {
  const text = stripComments(sql)
  return rules
    .filter((rule) => rule.pattern.test(text))
    .map(({ level, title, detail }) => ({ level, title, detail }))
}
