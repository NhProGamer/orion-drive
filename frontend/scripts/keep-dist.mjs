// Vite's build empties the output dir, deleting the committed .gitkeep that
// keeps application/statics/dist/ present for //go:embed on a fresh checkout.
// Recreate it after every build.
import { writeFileSync } from 'node:fs'

writeFileSync(new URL('../../application/statics/dist/.gitkeep', import.meta.url), '')
