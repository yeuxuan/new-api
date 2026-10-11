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
import { spawnSync } from 'node:child_process'
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const script = resolve(
  dirname(fileURLToPath(import.meta.url)),
  '../../../scripts/sync-i18n.mjs'
)
const attributionKey = 'footer.newapi.projectAttributionSuffix'
const escapedAttributionKey = 'footer.new\\u0061pi.projectAttributionSuffix'

describe('i18n synchronization preserves existing locale serialization', () => {
  it.each([
    { name: 'plain keys', enEscaped: false, zhEscaped: false },
    { name: 'escaped keys', enEscaped: true, zhEscaped: true },
    {
      name: 'different spelling in each locale',
      enEscaped: false,
      zhEscaped: true,
    },
  ])('$name', ({ enEscaped, zhEscaped }) => {
    const directory = mkdtempSync(join(tmpdir(), 'new-api-i18n-sync-'))
    const locales = join(directory, 'src/i18n/locales')
    mkdirSync(locales, { recursive: true })
    const inputs = [
      { locale: 'en', escaped: enEscaped },
      { locale: 'zh', escaped: zhEscaped },
    ]

    try {
      for (const { locale, escaped } of inputs) {
        const content = `${JSON.stringify(
          {
            translation: {
              [attributionKey]: `original ${locale} attribution`,
              'Existing custom message': `original ${locale} custom translation`,
            },
          },
          null,
          2
        )}\n`
        writeFileSync(
          join(locales, `${locale}.json`),
          escaped
            ? content.replace(attributionKey, escapedAttributionKey)
            : content
        )
      }

      const first = spawnSync(process.execPath, [script], {
        cwd: directory,
        encoding: 'utf8',
      })
      expect(first.error).toBeUndefined()
      expect(first.status, first.stderr).toBe(0)

      const synchronized = inputs.map(({ locale, escaped }) => {
        const content = readFileSync(join(locales, `${locale}.json`), 'utf8')
        expect(JSON.parse(content).translation).toEqual({
          [attributionKey]: `original ${locale} attribution`,
          'Existing custom message': `original ${locale} custom translation`,
        })
        expect(content).toContain(
          `"${escaped ? escapedAttributionKey : attributionKey}":`
        )
        expect(content).not.toContain(
          `"${escaped ? attributionKey : escapedAttributionKey}":`
        )
        return content
      })

      const second = spawnSync(process.execPath, [script], {
        cwd: directory,
        encoding: 'utf8',
      })
      expect(second.error).toBeUndefined()
      expect(second.status, second.stderr).toBe(0)
      expect(
        inputs.map(({ locale }) =>
          readFileSync(join(locales, `${locale}.json`), 'utf8')
        )
      ).toEqual(synchronized)
    } finally {
      rmSync(directory, { recursive: true, force: true })
    }
  })
})
