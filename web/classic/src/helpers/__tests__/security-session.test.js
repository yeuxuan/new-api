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
import { beforeAll, beforeEach, expect, mock, test } from 'bun:test';
import axios from 'axios';
import {
  applyAuthRotation,
  clearAuthSession,
  getAuthState,
  persistAuthResponse,
} from '../authSession';

const storage = new Map();
globalThis.localStorage = {
  getItem: (key) => storage.get(key) ?? null,
  setItem: (key, value) => storage.set(key, String(value)),
  removeItem: (key) => storage.delete(key),
};
mock.module('../utils', () => ({
  showError: mock(),
  formatMessageForAPI: (value) => value,
  isValidMessage: () => true,
}));

let respond;
const requests = [];
axios.defaults.adapter = async (config) => {
  requests.push(config);
  return respond(config);
};
let API;
let refreshAuthentication;
let getFreshAuthHeaders;
let logoutAuthentication;
beforeAll(async () => {
  ({ API, refreshAuthentication, getFreshAuthHeaders, logoutAuthentication } =
    await import('../api'));
});

function bundle(overrides = {}) {
  return {
    user: { id: 1, username: 'local-test', role: 1 },
    session: { sid: 'session-a', current: true },
    access_token: 'test-access',
    token_type: 'Bearer',
    access_expires_at: Math.floor(Date.now() / 1000) + 3600,
    ...overrides,
  };
}

function reply(config, data) {
  return { config, status: 200, statusText: 'OK', headers: {}, data };
}

function unauthorized(config) {
  return new axios.AxiosError(
    'Unauthorized',
    'ERR_BAD_REQUEST',
    config,
    {},
    {
      config,
      status: 401,
      statusText: 'Unauthorized',
      headers: {},
      data: {},
    },
  );
}

beforeEach(() => {
  clearAuthSession();
  requests.length = 0;
  respond = (config) => reply(config, { success: true });
});

test('keeps login tokens in memory and stores only user metadata', () => {
  const user = persistAuthResponse(bundle());
  expect(user).toEqual({
    id: 1,
    username: 'local-test',
    role: 1,
    session: { sid: 'session-a' },
  });
  expect(localStorage.getItem('user')).not.toContain('test-access');
  expect(getAuthState().access_token).toBe('test-access');
  clearAuthSession();
  expect(getAuthState()).toBeNull();
});

test('accepts rotation only for the active session and valid expiry', () => {
  persistAuthResponse(bundle());
  expect(() =>
    applyAuthRotation(bundle({ session: { sid: 'other', current: true } })),
  ).toThrow();
  expect(() => applyAuthRotation(bundle({ access_expires_at: NaN }))).toThrow();
  applyAuthRotation(bundle({ access_token: 'rotated' }));
  expect(getAuthState().access_token).toBe('rotated');
  expect(localStorage.getItem('user')).not.toContain('rotated');
});

test.each([true, undefined])(
  'refreshes before a proof request and never replays it (explicit flag: %s)',
  async (singleUseAuthorization) => {
    persistAuthResponse(bundle({ access_expires_at: 1 }));
    respond = (config) => {
      if (config.url === '/api/user/auth/refresh') {
        return reply(config, {
          success: true,
          data: bundle({ access_token: 'fresh' }),
        });
      }
      throw unauthorized(config);
    };
    await expect(
      API.post('/api/channel/1/key', undefined, {
        headers: { 'X-Security-Proof': 'proof' },
        singleUseAuthorization,
        skipErrorHandler: true,
      }),
    ).rejects.toThrow('Unauthorized');
    expect(requests.map((config) => config.url)).toEqual([
      '/api/user/auth/refresh',
      '/api/channel/1/key',
    ]);
    expect(requests[1].headers.Authorization).toBe('Bearer fresh');
    expect(requests[1].headers['X-Security-Proof']).toBe('proof');
  },
);

test('does not restore a session when refresh finishes after logout', async () => {
  persistAuthResponse(bundle());
  let release;
  let started;
  const ready = new Promise((resolve) => {
    started = resolve;
  });
  respond = (config) =>
    new Promise((resolve) => {
      release = () => resolve(reply(config, { success: true, data: bundle() }));
      started();
    });
  const refresh = refreshAuthentication();
  await ready;
  clearAuthSession();
  release();
  await expect(refresh).rejects.toThrow('superseded');
  expect(getAuthState()).toBeNull();
  expect(localStorage.getItem('user')).toBeNull();
});

test('does not deduplicate requests across two login sessions', async () => {
  const pending = [];
  let ready;
  const started = new Promise((resolve) => {
    ready = resolve;
  });
  respond = (config) =>
    new Promise((resolve) => {
      pending.push(() => resolve(reply(config, { success: true })));
      ready();
    });
  persistAuthResponse(bundle());
  const first = API.get('/api/user/self');
  await started;
  persistAuthResponse(bundle({ session: { sid: 'session-b', current: true } }));
  const second = API.get('/api/user/self');
  expect(first).not.toBe(second);
  await new Promise((resolve) => {
    ready = resolve;
  });
  pending.forEach((release) => release());
  await Promise.all([first, second]);
  expect(requests).toHaveLength(2);
});

test('refreshes streaming credentials without persisting the bearer token', async () => {
  persistAuthResponse(bundle({ access_expires_at: 1 }));
  respond = (config) =>
    reply(config, {
      success: true,
      data: bundle({ access_token: 'stream-token' }),
    });
  expect(await getFreshAuthHeaders()).toEqual({
    'Content-Type': 'application/json',
    'New-API-User': '1',
    Authorization: 'Bearer stream-token',
  });
  await getFreshAuthHeaders();
  expect(requests.map((request) => request.url)).toEqual([
    '/api/user/auth/refresh',
  ]);
  expect(localStorage.getItem('user')).not.toContain('stream-token');
});

test('logs out through the new scoped endpoint even when the access token expired', async () => {
  persistAuthResponse(bundle({ access_expires_at: 1 }));
  await logoutAuthentication();
  expect(requests).toHaveLength(1);
  expect(requests[0].method).toBe('post');
  expect(requests[0].url).toBe('/api/user/auth/logout');
  expect(requests[0].headers['X-Auth-Session']).toBe('session-a');
  expect(getAuthState()).toBeNull();
  expect(localStorage.getItem('user')).toBeNull();
});

test('does not pretend logout succeeded when the server cannot revoke the session', async () => {
  persistAuthResponse(bundle());
  respond = () => {
    throw new Error('server unavailable');
  };
  await expect(logoutAuthentication()).rejects.toThrow('server unavailable');
  expect(getAuthState().session.sid).toBe('session-a');
});

test('does not clear a newly logged-in session when an earlier logout finishes', async () => {
  persistAuthResponse(bundle());
  let release;
  let started;
  const ready = new Promise((resolve) => {
    started = resolve;
  });
  respond = (config) =>
    new Promise((resolve) => {
      release = () => resolve(reply(config, { success: true }));
      started();
    });
  const logout = logoutAuthentication();
  await ready;
  persistAuthResponse(bundle({ session: { sid: 'session-b', current: true } }));
  release();
  await expect(logout).rejects.toThrow();
  expect(getAuthState().session.sid).toBe('session-b');
});
