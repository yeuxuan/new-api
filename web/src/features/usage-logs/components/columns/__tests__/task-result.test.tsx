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

import { describe, test } from 'vitest'

import type { TaskLog } from '../../../types'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { api } = await import('@/lib/http-client')
const { useAuthStore } = await import('@/stores/auth-store')
const { TaskDetailsCell } = await import('../task-logs-columns')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'No result is available yet': 'No result is available yet',
        'Open result': 'Open result',
        'Public task data': 'Public task data',
        'Result Preview': 'Result Preview',
        'Task Details': 'Task Details',
        'Task information and generated result':
          'Task information and generated result',
        'TMLab Seedance': 'TMLab Seedance',
        'Video preview could not be loaded':
          'Video preview could not be loaded',
        'You can retry after checking the task result.':
          'You can retry after checking the task result.',
        Retry: 'Retry',
        'Video result': 'Video result',
        'View details': 'View details',
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
    platform: '61',
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
  test('opens task details and loads an inline video with dashboard authentication', async () => {
    useAuthStore.getState().auth.setBundle({
      access_token: 'dashboard-session-token',
      token_type: 'Bearer',
      access_expires_at: 2_000_000_000,
      user: { id: 1, username: 'root', role: 100 },
      session: {
        sid: 'test-session',
        current: true,
        login_method: 'password',
        ip: '127.0.0.1',
        user_agent: 'test',
        created_at: 1,
        last_active_at: 1,
        expires_at: 2_000_000_000,
      },
    })

    let requestUrl = ''
    let requestAuthorization = ''
    let responseType = ''
    const originalAdapter = api.defaults.adapter
    api.defaults.adapter = async (config) => {
      requestUrl = config.url ?? ''
      requestAuthorization = String(config.headers.Authorization ?? '')
      responseType = config.responseType ?? ''
      return {
        data: new Blob(['video'], { type: 'video/mp4' }),
        status: 200,
        statusText: 'OK',
        headers: { 'content-type': 'video/mp4' },
        config,
      }
    }

    const originalCreateObjectURL = URL.createObjectURL
    const originalRevokeObjectURL = URL.revokeObjectURL
    const revokedUrls: string[] = []
    URL.createObjectURL = () => 'blob:authenticated-video-result'
    URL.revokeObjectURL = (url) => revokedUrls.push(String(url))

    const rendered = await renderDetails(
      createTaskLog({ result_url: 'https://upstream.example/result.mp4' })
    )

    const button = rendered.container.querySelector('button')
    assert.ok(button)
    assert.equal(button.textContent, 'View details')

    await act(async () => {
      button.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    assert.equal(requestUrl, '/v1/videos/task_result%2Fwith%20space/content')
    assert.equal(requestAuthorization, 'Bearer dashboard-session-token')
    assert.equal(responseType, 'blob')
    const sheet = document.querySelector('[data-slot="sheet-content"]')
    assert.ok(sheet)
    assert.match(sheet.textContent ?? '', /Task Details/)
    assert.match(sheet.textContent ?? '', /Result Preview/)
    assert.match(sheet.textContent ?? '', /Public task data/)
    assert.match(sheet.textContent ?? '', /TMLab Seedance/)
    assert.match(sheet.textContent ?? '', /task_result\/with space/)
    const video = sheet.querySelector('video')
    assert.ok(video)
    assert.equal(video.getAttribute('src'), 'blob:authenticated-video-result')
    assert.equal(
      sheet.textContent?.includes('https://upstream.example/result.mp4'),
      true
    )

    await unmountDetails(rendered)
    assert.deepEqual(revokedUrls, ['blob:authenticated-video-result'])
    api.defaults.adapter = originalAdapter
    URL.createObjectURL = originalCreateObjectURL
    URL.revokeObjectURL = originalRevokeObjectURL
    useAuthStore.getState().auth.reset()
  })

  test('shows task details without requesting a video before a result is available', async () => {
    let requestCount = 0
    const originalAdapter = api.defaults.adapter
    api.defaults.adapter = async (config) => {
      requestCount++
      return {
        data: new Blob(),
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    const rendered = await renderDetails(
      createTaskLog({ status: 'IN_PROGRESS' })
    )

    const button = rendered.container.querySelector('button')
    assert.ok(button)
    assert.equal(button.textContent, 'View details')

    await act(async () => {
      button.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    const sheet = document.querySelector('[data-slot="sheet-content"]')
    assert.ok(sheet)
    assert.match(sheet.textContent ?? '', /No result is available yet/)
    assert.equal(sheet.querySelector('video'), null)
    assert.equal(requestCount, 0)

    await unmountDetails(rendered)
    api.defaults.adapter = originalAdapter
  })

  test('shows a recoverable error and retries the video request', async () => {
    let requestCount = 0
    const originalAdapter = api.defaults.adapter
    api.defaults.adapter = async (config) => {
      requestCount++
      if (requestCount === 1) throw new Error('temporary preview failure')
      return {
        data: new Blob(['video'], { type: 'video/mp4' }),
        status: 200,
        statusText: 'OK',
        headers: { 'content-type': 'video/mp4' },
        config,
      }
    }
    const originalCreateObjectURL = URL.createObjectURL
    URL.createObjectURL = () => 'blob:retried-video-result'
    const rendered = await renderDetails(
      createTaskLog({ result_url: 'https://upstream.example/result.mp4' })
    )

    const detailsButton = rendered.container.querySelector('button')
    assert.ok(detailsButton)
    await act(async () => {
      detailsButton.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    const sheet = document.querySelector('[data-slot="sheet-content"]')
    assert.ok(sheet)
    assert.match(sheet.textContent ?? '', /Video preview could not be loaded/)
    const retryButton = [...sheet.querySelectorAll('button')].find(
      (button) => button.textContent === 'Retry'
    )
    assert.ok(retryButton)
    await act(async () => {
      retryButton.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    assert.equal(requestCount, 2)
    const video = sheet.querySelector('video')
    assert.ok(video)
    assert.equal(video.getAttribute('src'), 'blob:retried-video-result')

    await unmountDetails(rendered)
    api.defaults.adapter = originalAdapter
    URL.createObjectURL = originalCreateObjectURL
  })
})
