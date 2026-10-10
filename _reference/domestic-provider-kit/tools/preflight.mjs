import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import { fileURLToPath } from 'node:url'

const kit = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const target = path.resolve(process.argv[2] || '.')
const manifest = JSON.parse(fs.readFileSync(path.join(kit, 'SOURCE-MANIFEST.json'), 'utf8'))
if (!fs.existsSync(path.join(target, 'backend/go.mod')) || !fs.existsSync(path.join(target, 'frontend/package.json'))) {
  console.error('Target must contain backend/go.mod and frontend/package.json. No files changed.')
  process.exit(1)
}
const rows = manifest.files.filter((f) => ['core', 'reference/shared', 'reference/protocol', 'migrations'].includes(f.category)).map((f) => {
  const dest = path.join(target, f.source)
  const state = !fs.existsSync(dest) ? 'missing' : crypto.createHash('sha256').update(fs.readFileSync(dest)).digest('hex') === f.sha256 ? 'identical' : 'merge-required'
  return { path: f.source, category: f.category, state }
})
console.log(JSON.stringify({
  readOnly: true,
  target,
  sourceVersion: manifest.sourceVersion,
  files: rows,
  reminders: [
    'This is a path/content preflight, not a compatibility verdict.',
    'Do not overwrite shared reference files.',
    'Renumber migrations using the target migration history, even when paths are missing.',
    'Regenerate ent and wire in the target repository.',
    'Verify price policy, provider validation, protocol routing, and billing before deployment.'
  ]
}, null, 2))
