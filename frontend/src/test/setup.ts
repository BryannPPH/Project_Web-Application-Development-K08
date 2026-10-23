import '@testing-library/jest-dom/vitest'

import { configure } from '@testing-library/react'

// findBy*/waitFor default-nya 1 detik; saat banyak file test berjalan paralel di
// mesin yang sibuk, render jsdom bisa lebih lambat sehingga test menjadi flaky.
configure({ asyncUtilTimeout: 3000 })
