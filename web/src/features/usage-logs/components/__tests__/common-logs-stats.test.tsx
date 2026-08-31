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
import {
  onlineManager,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { act, cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { formatLogQuota } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'

import { CommonLogsStats } from '../common-logs-stats'
import { UsageLogsProvider } from '../usage-logs-provider'

function StatsPage() {
  return (
    <UsageLogsProvider>
      <CommonLogsStats />
    </UsageLogsProvider>
  )
}

describe('common log statistics', () => {
  let queryClient: QueryClient
  let previousUser: AuthUser | null
  let previousOnline: boolean

  beforeEach(() => {
    vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
    previousUser = useAuthStore.getState().auth.user
    previousOnline = onlineManager.isOnline()
    onlineManager.setOnline(true)
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
  })

  afterEach(() => {
    cleanup()
    queryClient.clear()
    onlineManager.setOnline(previousOnline)
    useAuthStore.getState().auth.setUser(previousUser)
  })

  function renderStats(role: number = ROLE.USER) {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'alice', role })
    const root = createRootRoute({ component: Outlet })
    const authenticated = createRoute({
      getParentRoute: () => root,
      id: '_authenticated',
      component: Outlet,
    })
    const logs = createRoute({
      getParentRoute: () => authenticated,
      path: 'usage-logs/$section',
      component: StatsPage,
    })
    const router = createRouter({
      routeTree: root.addChildren([authenticated.addChildren([logs])]),
      history: createMemoryHistory({ initialEntries: ['/usage-logs/common'] }),
    })
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
  }

  test.each([
    { role: ROLE.ADMIN, path: '/api/log/stat?' },
    { role: ROLE.USER, path: '/api/log/self/stat?' },
  ])('displays quota and rates from $path', async ({ role, path }) => {
    const request = vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { quota: 44000, rpm: 3, tpm: 117 } },
    })
    renderStats(role)

    expect(await screen.findByText(formatLogQuota(44000))).toBeVisible()
    expect(screen.getByText('3')).toBeVisible()
    expect(screen.getByText('117')).toBeVisible()
    expect(request).toHaveBeenCalledWith(expect.stringContaining(path))
  })

  test.each([
    { name: 'API failure', response: { success: false, message: 'DB failed' } },
    { name: 'missing data', response: { success: true } },
    {
      name: 'incomplete data',
      response: { success: true, data: { rpm: 1, tpm: 120 } },
    },
  ])('shows an error instead of zero for $name', async ({ response }) => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: response })
    renderStats()

    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to load')
    expect(screen.queryByText(formatLogQuota(0))).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeEnabled()
  })

  test('shows an error instead of zero when the network request fails', async () => {
    vi.spyOn(api, 'get').mockRejectedValue(new Error('Network unavailable'))
    renderStats()

    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to load')
    expect(screen.queryByText(formatLogQuota(0))).not.toBeInTheDocument()
  })

  test('does not display zero while an offline query is paused', async () => {
    onlineManager.setOnline(false)
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { quota: 500000, rpm: 1, tpm: 120 } },
    })
    renderStats()

    expect(
      await screen.findByRole('status', { name: 'Loading...' })
    ).toBeVisible()
    expect(screen.queryByText(formatLogQuota(0))).not.toBeInTheDocument()

    await act(async () => onlineManager.setOnline(true))
    expect(await screen.findByText(formatLogQuota(500000))).toBeVisible()
  })

  test('still displays zero when the server successfully reports no usage', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { quota: 0, rpm: 0, tpm: 0 } },
    })
    renderStats()

    expect(await screen.findByText(formatLogQuota(0))).toBeVisible()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  test('retries a failed query and replaces the error with actual usage', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce({ data: { success: false } })
      .mockResolvedValue({
        data: { success: true, data: { quota: 500000, rpm: 1, tpm: 120 } },
      })
    renderStats()

    await user.click(await screen.findByRole('button', { name: 'Retry' }))

    expect(await screen.findByText(formatLogQuota(500000))).toBeVisible()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  test('does not present cached statistics as current when refreshing fails', async () => {
    const request = vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { quota: 500000, rpm: 1, tpm: 120 } },
    })
    renderStats()
    expect(await screen.findByText(formatLogQuota(500000))).toBeVisible()

    request.mockRejectedValue(new Error('Network unavailable'))
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['usage-logs-stats'] })
    })

    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to load')
    expect(screen.queryByText(formatLogQuota(500000))).not.toBeInTheDocument()
  })
})
