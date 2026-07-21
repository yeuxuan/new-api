/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import fs from 'node:fs/promises'
import path from 'node:path'

const source = await fs.readFile(
  path.resolve('src/features/seo/claude-code-content.ts'),
  'utf8'
)
const contentStart = source.indexOf('export const CLAUDE_CODE_SECTIONS')
if (contentStart < 0) throw new Error('Claude Code SEO content was not found')

const contentKeys = [...source.slice(contentStart).matchAll(/'([^']+)'/g)].map(
  (match) => match[1]
)
const zh = JSON.parse(
  await fs.readFile(path.resolve('src/i18n/locales/zh.json'), 'utf8')
).translation

const english = contentKeys.join(' ')
const chinese = contentKeys.map((key) => zh[key] ?? '').join('')
const englishWords =
  english.match(/[A-Za-z0-9]+(?:[’-][A-Za-z0-9]+)*/g)?.length ?? 0
const chineseCharacters = chinese.match(/[\p{Script=Han}]/gu)?.length ?? 0

const targets = [
  ['English words', englishWords],
  ['Chinese Han characters', chineseCharacters],
]
let failed = false
for (const [label, count] of targets) {
  const passes = count >= 1200 && count <= 1800
  console.log(
    `${passes ? 'PASS' : 'FAIL'} ${label}: ${count} (target 1200-1800)`
  )
  failed ||= !passes
}

const semanticTerms = [
  'Claude Code API',
  'API proxy',
  'live pricing',
  'model health',
  'Anthropic',
  'OpenAI-compatible',
]
for (const term of semanticTerms) {
  const occurrences = english.toLowerCase().split(term.toLowerCase()).length - 1
  const passes = occurrences > 0
  console.log(
    `${passes ? 'PASS' : 'FAIL'} semantic term "${term}": ${occurrences}`
  )
  failed ||= !passes
}

if (failed) process.exitCode = 1
