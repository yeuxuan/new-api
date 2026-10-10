/*
Copyright (C) 2025 QuantumNous

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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { useLogsData } from '../useUsageLogsData';
import { API, isAdmin, showError } from '../../../helpers';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key) => key }),
}));
vi.mock('../../../helpers', () => ({
  API: { get: vi.fn() },
  isAdmin: vi.fn(),
  showError: vi.fn(),
  getTodayStartTimestamp: () => 1700000000,
  timestamp2string: (value) => new Date(value * 1000).toISOString(),
  getTableCompactMode: () => false,
}));

const special = 'audit+A&B#1 中文/100%?';
const filters = {
  username: special,
  token_name: special,
  model_name: special,
  channel: '7',
  group: special,
  request_id: special,
  logType: '2',
  dateRange: ['2026-09-01T00:00:00Z', '2026-09-02T00:00:00Z'],
};
const stat = { quota: 1234567, rpm: 15, tpm: 1907376 };

beforeEach(() => {
  localStorage.clear();
  API.get.mockReset().mockImplementation(async (url) => {
    const pathname = new URL(url, 'http://localhost').pathname;
    if (pathname.endsWith('/stat'))
      return { data: { success: true, data: stat } };
    if (pathname.endsWith('/export')) return { data: 'csv-data' };
    return {
      data: {
        success: true,
        data: { items: [], page: 1, page_size: 20, total: 0 },
      },
    };
  });
  URL.createObjectURL = vi.fn(() => 'blob:export');
  URL.revokeObjectURL = vi.fn();
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
});
afterEach(cleanup);

describe('Classic usage log filter contracts', () => {
  it.each([false, true])(
    'preserves filters for list, statistics and export (admin=%s)',
    async (admin) => {
      isAdmin.mockReturnValue(admin);
      const { result } = renderHook(() => useLogsData());
      await waitFor(() => expect(result.current.loading).toBe(false));
      await act(async () => {
        result.current.setFormApi({ getValues: () => filters });
      });
      await waitFor(() => expect(result.current.stat).toEqual(stat));
      await act(async () => {
        await result.current.loadLogs(2, 50);
        await result.current.exportLogs();
      });

      const urls = API.get.mock.calls.map(
        ([url]) => new URL(url, 'http://localhost'),
      );
      const base = admin ? '/api/log' : '/api/log/self';
      for (const suffix of ['/', '/stat', '/export']) {
        const url = urls.findLast((item) => item.pathname === base + suffix);
        expect(url.hash).toBe('');
        for (const field of ['token_name', 'model_name', 'group']) {
          expect(url.searchParams.get(field)).toBe(special);
        }
        expect(url.searchParams.get('type')).toBe('2');
        expect(url.searchParams.get('start_timestamp')).toBe(
          String(Date.parse(filters.dateRange[0]) / 1000),
        );
        expect(url.searchParams.get('end_timestamp')).toBe(
          String(Date.parse(filters.dateRange[1]) / 1000),
        );
        if (suffix !== '/stat')
          expect(url.searchParams.get('request_id')).toBe(special);
        expect(url.searchParams.get('username')).toBe(admin ? special : null);
        expect(url.searchParams.get('channel')).toBe(admin ? '7' : null);
      }
      const list = urls.findLast((item) => item.pathname === base + '/');
      expect(list.searchParams.get('p')).toBe('2');
      expect(list.searchParams.get('page_size')).toBe('50');
      expect(showError).not.toHaveBeenCalled();
    },
  );

  it('unlocks statistics after a failed request so the next search can recover', async () => {
    isAdmin.mockReturnValue(false);
    const { result } = renderHook(() => useLogsData());
    await waitFor(() => expect(result.current.loading).toBe(false));
    API.get.mockRejectedValueOnce(new Error('temporary network failure'));
    await act(async () => {
      await result.current.handleEyeClick();
    });
    expect(result.current.loadingStat).toBe(false);
    expect(showError).toHaveBeenCalledOnce();
    await act(async () => {
      await result.current.handleEyeClick();
    });
    expect(result.current.stat).toEqual(stat);
  });
});
