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
import assert from 'node:assert/strict'
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'

import type { TaskLog } from '../../../types'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { TaskDetailsCell } = await import('../task-logs-columns')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'View result': 'View result',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function createTaskLog(overrides: Partial<TaskLog> = {}): TaskLog {
  return {
    id: 1,
    user_id: 1,
    platform: 'tmlab-seedance',
    task_id: 'task_result/with space',
    action: 'generate',
    channel_id: 64,
    submit_time: 1,
    status: 'SUCCESS',
    ...overrides,
  }
}

async function renderDetails(log: TaskLog) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <TaskDetailsCell log={log} />
      </I18nextProvider>
    )
  })

  return { container, root }
}

async function unmountDetails(
  rendered: Awaited<ReturnType<typeof renderDetails>>
) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

describe('task log result details', () => {
  after(() => {
    domWindow.close()
  })

  test('shows the authenticated result link when a completed video task has result_url', async () => {
    const rendered = await renderDetails(
      createTaskLog({ result_url: 'https://upstream.example/result.mp4' })
    )

    const link = rendered.container.querySelector('a')
    assert.ok(link)
    assert.equal(link.textContent, 'View result')
    assert.equal(
      link.getAttribute('href'),
      '/v1/videos/task_result%2Fwith%20space/content'
    )
    assert.equal(link.getAttribute('target'), '_blank')
    assert.equal(
      rendered.container.textContent?.includes('upstream.example'),
      false
    )

    await unmountDetails(rendered)
  })

  test('does not show a result link before a video result is available', async () => {
    const rendered = await renderDetails(
      createTaskLog({ status: 'IN_PROGRESS' })
    )

    assert.equal(rendered.container.querySelector('a'), null)
    assert.equal(rendered.container.textContent, '-')

    await unmountDetails(rendered)
  })
})
