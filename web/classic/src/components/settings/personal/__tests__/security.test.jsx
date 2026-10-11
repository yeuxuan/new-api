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

import '@douyinfe/semi-ui/react19-adapter';

import React from 'react';
import { Modal, Toast } from '@douyinfe/semi-ui';
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import PersonalSetting from '../../PersonalSetting';
import { StatusContext } from '../../../../context/Status';
import { UserContext } from '../../../../context/User';
import { API } from '../../../../helpers/api';
import * as authentication from '../../../../helpers/api';
import { SecureVerificationService } from '../../../../services/secureVerification';
import {
  clearAuthSession,
  getAuthState,
  getStoredUser,
  persistAuthResponse,
} from '../../../../helpers/authSession';

const { navigate } = vi.hoisted(() => ({ navigate: vi.fn() }));

vi.mock('react-router-dom', () => ({
  useNavigate: () => navigate,
}));

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key, values) => {
      if (!values) return key;
      return key.replace(/\{\{(\w+)\}\}/g, (_, name) => values[name] ?? '');
    },
    i18n: { language: 'zh-CN', changeLanguage: vi.fn() },
  }),
}));

let requests;
let user;
let method;
let respond;
let originalAdapter;
let accessTokens;

function success(data) {
  return { success: true, data };
}

function rotation() {
  return {
    access_token: 'rotated-session-token',
    token_type: 'Bearer',
    access_expires_at: Math.floor(Date.now() / 1000) + 3600,
    session: { sid: 'personal-session', current: true },
  };
}

beforeEach(() => {
  requests = [];
  method = 'password';
  respond = () => undefined;
  accessTokens = {
    items: [],
    legacy: null,
  };
  user = {
    id: 9,
    username: 'personal-user',
    role: 1,
    has_password: true,
    email: '',
    setting: '{}',
    quota: 500000,
  };
  localStorage.clear();
  localStorage.setItem('quota_per_unit', '500000');
  persistAuthResponse({ ...rotation(), user });
  Object.defineProperty(window, 'PublicKeyCredential', {
    configurable: true,
    value: class {},
  });
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
  });
  Object.defineProperty(navigator, 'credentials', {
    configurable: true,
    value: {
      get: vi.fn(),
      create: vi.fn().mockResolvedValue({
        id: 'new-passkey',
        rawId: new ArrayBuffer(1),
        type: 'public-key',
        response: {
          attestationObject: new ArrayBuffer(1),
          clientDataJSON: new ArrayBuffer(1),
        },
        getClientExtensionResults: () => ({}),
      }),
    },
  });
  originalAdapter = API.defaults.adapter;
  API.defaults.adapter = async (config) => {
    const body = config.data ? JSON.parse(config.data) : undefined;
    const request = { url: config.url, method: config.method, body, config };
    requests.push(request);
    let data = await respond(request);
    if (!data) {
      switch (config.url) {
        case '/api/status':
          data = success({
            email_bind_reward_enabled: true,
            wechat_login: true,
            quota_per_unit: 500000,
          });
          break;
        case '/api/user/self':
          data = success(user);
          break;
        case '/api/user/passkey':
          data = success({ enabled: false });
          break;
        case '/api/user/oauth/bindings':
          data = success([]);
          break;
        case '/api/user/2fa/status':
          data = success({ enabled: false });
          break;
        case '/api/user/access_tokens/catalog':
          data = success({
            default_expiry_days: 30,
            max_tokens: 10,
            groups: [
              {
                group: 'personal',
                resources: [
                  {
                    resource: 'profile',
                    label_key: 'Profile',
                    actions: [
                      { action: 'read', label_key: 'View' },
                      { action: 'write', label_key: 'Edit' },
                    ],
                  },
                ],
              },
            ],
          });
          break;
        case '/api/user/access_tokens':
          if (config.method !== 'get') {
            throw new Error(`Unexpected token mutation: ${config.method}`);
          }
          data = success(accessTokens);
          break;
        case '/api/verify/methods':
          data = success({
            scope: config.params.scope,
            methods: [{ method, available: true }],
          });
          break;
        case '/api/verify':
          data = success({
            proof_token: 'one-use-proof',
            scope: body.scope,
            method: body.method,
            expires_at: Math.floor(Date.now() / 1000) + 60,
          });
          break;
        default:
          throw new Error(
            `Unexpected personal request: ${config.method} ${config.url}`,
          );
      }
    }
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
});

afterEach(() => {
  cleanup();
  Toast.destroyAll();
  API.defaults.adapter = originalAdapter;
  clearAuthSession();
});

async function openPersonalSettings() {
  let updateUser;
  function Account() {
    const [state, dispatch] = React.useReducer(
      (previous, action) => ({
        user: action.type === 'login' ? action.payload : undefined,
      }),
      { user: { ...user, session: { sid: 'personal-session' } } },
    );
    updateUser = dispatch;
    return (
      <UserContext.Provider value={[state, dispatch]}>
        <StatusContext.Provider value={[{ status: {} }, vi.fn()]}>
          <PersonalSetting />
        </StatusContext.Provider>
      </UserContext.Provider>
    );
  }
  const result = render(<Account />);
  await waitFor(() =>
    expect(requests.some((request) => request.url === '/api/user/self')).toBe(
      true,
    ),
  );
  return {
    ...result,
    switchSession(nextUser, sid) {
      persistAuthResponse({ ...rotation(), user: nextUser, session: { sid } });
      updateUser({ type: 'login', payload: { ...nextUser, session: { sid } } });
    },
  };
}

async function securityTab() {
  fireEvent.click(
    await screen.findByRole('tab', { name: '安全设置', exact: true }),
  );
}

async function verifyPassword() {
  const input = await screen.findByPlaceholderText('请输入当前密码');
  fireEvent.change(input, { target: { value: 'current-password' } });
  fireEvent.click(screen.getByRole('button', { name: '验证', exact: true }));
}

function confirmDialog() {
  const dialog = screen.getByRole('dialog');
  fireEvent.click(
    within(dialog).getByRole('button', { name: /确\s*定|confirm|OK/ }),
  );
}

async function selectTokenGrant() {
  fireEvent.click(
    screen.getByRole('button', { name: 'Create access token', exact: true }),
  );
  await screen.findByLabelText('Token name');
  const dialog = screen.getByRole('dialog');
  fireEvent.change(screen.getByLabelText('Token name'), {
    target: { value: 'Classic CLI' },
  });
  expect(
    screen.getByRole('checkbox', { name: 'Profile / View' }),
  ).not.toBeChecked();
  expect(
    screen.getByRole('checkbox', { name: 'Profile / Edit' }),
  ).not.toBeChecked();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Profile / View' }));
  fireEvent.click(screen.getByRole('radio', { name: 'Valid for 90 days' }));
  fireEvent.click(
    within(dialog).getByRole('button', {
      name: 'Create access token',
      exact: true,
    }),
  );
}

test('personal token creation binds the selected grant and expiry to the proof and keeps plaintext out of storage', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1800000000000);
  persistAuthResponse({ ...rotation(), user });
  respond = ({ url, method }) =>
    url === '/api/user/access_tokens' && method === 'post'
      ? success({ token: 'personal-access-token' })
      : undefined;
  await openPersonalSettings();
  await securityTab();
  await selectTokenGrant();
  await screen.findByPlaceholderText('请输入当前密码');
  expect(
    requests.filter(
      ({ url, method }) =>
        url === '/api/user/access_tokens' && method === 'post',
    ),
  ).toHaveLength(0);
  await verifyPassword();
  await screen.findByDisplayValue('personal-access-token');
  const request = requests.find(
    ({ url, method }) => url === '/api/user/access_tokens' && method === 'post',
  );
  expect(request.method).toBe('post');
  expect(request.body).toEqual({
    name: 'Classic CLI',
    scopes: ['profile:read'],
    expires_at: 1800000000 + 90 * 86400,
  });
  expect(request.config.headers.get('X-Security-Proof')).toBe('one-use-proof');
  expect(request.config.singleUseAuthorization).toBe(true);
  expect(requests.find(({ url }) => url === '/api/verify').body).toMatchObject({
    method: 'password',
    password: 'current-password',
    scope: 'access_token.generate',
    context: {
      scopes: request.body.scopes,
      expires_at: request.body.expires_at,
    },
  });
  expect(localStorage.getItem('user')).not.toContain('personal-access-token');
  expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
    'personal-access-token',
  );
  expect(requests.some(({ url }) => url === '/api/user/token')).toBe(false);
  expect(JSON.stringify(sessionStorage)).not.toContain('personal-access-token');
});

test('cancelling token verification sends no token mutation', async () => {
  await openPersonalSettings();
  await securityTab();
  await selectTokenGrant();
  await screen.findByPlaceholderText('请输入当前密码');
  const verificationDialog = screen
    .getByPlaceholderText('请输入当前密码')
    .closest('[role="dialog"]');
  fireEvent.click(
    within(verificationDialog).getByRole('button', { name: /取消|cancel/ }),
  );
  expect(
    requests.filter(
      ({ url, method }) =>
        url === '/api/user/access_tokens' && method === 'post',
    ),
  ).toHaveLength(0);
});

test('the token form only offers catalog permissions and requires an explicit selection', async () => {
  await openPersonalSettings();
  await securityTab();
  fireEvent.click(
    screen.getByRole('button', { name: 'Create access token', exact: true }),
  );
  await screen.findByLabelText('Token name');
  fireEvent.change(screen.getByLabelText('Token name'), {
    target: { value: 'CLI' },
  });
  const dialog = screen.getByRole('dialog');
  expect(within(dialog).getAllByRole('checkbox')).toHaveLength(2);
  expect(
    within(dialog).getByRole('button', {
      name: 'Create access token',
      exact: true,
    }),
  ).toBeDisabled();
  fireEvent.click(
    within(dialog).getByRole('button', { name: '取消', exact: true }),
  );
  expect(requests.some(({ url }) => url === '/api/verify')).toBe(false);
  expect(
    requests.some(
      ({ url, method }) =>
        url === '/api/user/access_tokens' && method === 'post',
    ),
  ).toBe(false);
});

test('a failed catalog load does not start verification or use a fallback full grant', async () => {
  respond = ({ url }) =>
    url === '/api/user/access_tokens/catalog'
      ? { success: false, message: 'catalog unavailable' }
      : undefined;
  await openPersonalSettings();
  await securityTab();
  fireEvent.click(
    screen.getByRole('button', { name: 'Create access token', exact: true }),
  );
  await screen.findByText(/catalog unavailable/);
  expect(screen.queryByLabelText('Token name')).toBeNull();
  expect(requests.some(({ url }) => url === '/api/verify/methods')).toBe(false);
  expect(
    requests.some(
      ({ url, method }) =>
        url === '/api/user/access_tokens' && method === 'post',
    ),
  ).toBe(false);
});

test('a rejected security proof cannot create a token', async () => {
  respond = ({ url }) =>
    url === '/api/verify'
      ? { success: false, code: 'AUTH_PROOF_EXPIRED', message: 'proof expired' }
      : undefined;
  await openPersonalSettings();
  await securityTab();
  await selectTokenGrant();
  await verifyPassword();
  await screen.findByText(/proof expired/);
  expect(
    requests.some(
      ({ url, method }) =>
        url === '/api/user/access_tokens' && method === 'post',
    ),
  ).toBe(false);
});

function managedToken(id = 7) {
  return {
    id,
    name: `CLI ${id}`,
    token_hint: 'abcd',
    scopes: ['profile:read', 'retired:read'],
    expires_at: 1900000000,
    created_at: 1800000000,
    last_used_at: 0,
  };
}

async function openTokenEditor() {
  fireEvent.click(await screen.findByRole('button', { name: 'Edit CLI 7' }));
  return screen.findByRole('dialog', { name: 'Edit access token' });
}

test('lists all twenty tokens and allows revocation to free a slot with an ID-bound one-use proof', async () => {
  accessTokens.items = Array.from({ length: 20 }, (_, index) =>
    managedToken(index + 1),
  );
  respond = ({ url, method }) => {
    if (url === '/api/user/access_tokens/7' && method === 'delete') {
      accessTokens = {
        ...accessTokens,
        items: accessTokens.items.filter((item) => item.id !== 7),
      };
      return success();
    }
  };
  await openPersonalSettings();
  await securityTab();
  expect(await screen.findByText('CLI 20')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Revoke CLI 7' }));
  const dialog = await screen.findByRole('dialog', { name: 'Revoke' });
  expect(within(dialog).getByText('CLI 7')).toBeVisible();
  fireEvent.click(
    within(dialog).getByRole('button', { name: 'Revoke', exact: true }),
  );
  await verifyPassword();
  await waitFor(() =>
    expect(screen.queryByText('CLI 7')).not.toBeInTheDocument(),
  );
  const request = requests.find(({ method }) => method === 'delete');
  expect(request.url).toBe('/api/user/access_tokens/7');
  expect(request.config.headers.get('X-Security-Proof')).toBe('one-use-proof');
  expect(request.config.singleUseAuthorization).toBe(true);
  expect(requests.find(({ url }) => url === '/api/verify').body).toMatchObject({
    scope: 'access_token.revoke',
    context: { token_id: 7 },
  });
});

test('renames without verification and preserves stored permissions no longer in the catalog', async () => {
  accessTokens.items = [managedToken()];
  respond = ({ url, method, body }) => {
    if (url === '/api/user/access_tokens/7' && method === 'patch') {
      accessTokens = {
        ...accessTokens,
        items: [{ ...managedToken(), name: body.name }],
      };
      return success(accessTokens.items[0]);
    }
  };
  await openPersonalSettings();
  await securityTab();
  const dialog = await openTokenEditor();
  fireEvent.change(within(dialog).getByLabelText('Token name'), {
    target: { value: 'Renamed CLI' },
  });
  fireEvent.click(
    within(dialog).getByRole('button', { name: 'Save', exact: true }),
  );
  expect(await screen.findByText('Renamed CLI')).toBeVisible();
  const request = requests.find(({ method }) => method === 'patch');
  expect(request.body).toEqual({ name: 'Renamed CLI' });
  expect(request.config.headers.has('X-Security-Proof')).toBe(false);
  expect(requests.some(({ url }) => url === '/api/verify')).toBe(false);
});

test('permission changes require proof bound to the exact token and sorted replacement grant', async () => {
  accessTokens.items = [managedToken()];
  respond = ({ url, method, body }) => {
    if (url === '/api/user/access_tokens/7' && method === 'patch') {
      accessTokens = {
        ...accessTokens,
        items: [{ ...managedToken(), ...body }],
      };
      return success(accessTokens.items[0]);
    }
  };
  await openPersonalSettings();
  await securityTab();
  const dialog = await openTokenEditor();
  fireEvent.click(
    within(dialog).getByRole('checkbox', { name: 'Profile / Edit' }),
  );
  fireEvent.click(
    within(dialog).getByRole('button', { name: 'Save', exact: true }),
  );
  await screen.findByPlaceholderText('请输入当前密码');
  expect(requests.some(({ method }) => method === 'patch')).toBe(false);
  await verifyPassword();
  await waitFor(() =>
    expect(requests.some(({ method }) => method === 'patch')).toBe(true),
  );
  const request = requests.find(({ method }) => method === 'patch');
  expect(request.body).toEqual({
    name: 'CLI 7',
    scopes: ['profile:read', 'profile:write'],
  });
  expect(request.config.singleUseAuthorization).toBe(true);
  expect(request.config.headers.get('X-Security-Proof')).toBe('one-use-proof');
  expect(requests.find(({ url }) => url === '/api/verify').body).toMatchObject({
    scope: 'access_token.update',
    context: { token_id: 7, scopes: request.body.scopes },
  });
});

test.each(['confirmation', 'verification'])(
  'cancelling token revocation at %s sends no DELETE',
  async (phase) => {
    accessTokens.items = [managedToken()];
    await openPersonalSettings();
    await securityTab();
    fireEvent.click(
      await screen.findByRole('button', { name: 'Revoke CLI 7' }),
    );
    const dialog = await screen.findByRole('dialog', { name: 'Revoke' });
    if (phase === 'verification') {
      fireEvent.click(
        within(dialog).getByRole('button', { name: 'Revoke', exact: true }),
      );
      await screen.findByPlaceholderText('请输入当前密码');
    }
    const activeDialog =
      phase === 'verification'
        ? screen
            .getByPlaceholderText('请输入当前密码')
            .closest('[role="dialog"]')
        : dialog;
    fireEvent.click(
      within(activeDialog).getByRole('button', { name: /取消|cancel/ }),
    );
    expect(requests.some(({ method }) => method === 'delete')).toBe(false);
    expect(screen.getByText('CLI 7')).toBeVisible();
  },
);

test.each(['verification', 'mutation'])(
  'failed %s keeps the token and never replays a revoke',
  async (phase) => {
    accessTokens.items = [managedToken()];
    respond = ({ url, method }) => {
      if (
        (phase === 'verification' && url === '/api/verify') ||
        (phase === 'mutation' && method === 'delete')
      ) {
        return {
          success: false,
          message: 'token operation rejected',
          code: 'AUTH_PROOF_EXPIRED',
        };
      }
    };
    await openPersonalSettings();
    await securityTab();
    fireEvent.click(
      await screen.findByRole('button', { name: 'Revoke CLI 7' }),
    );
    const dialog = await screen.findByRole('dialog', { name: 'Revoke' });
    fireEvent.click(
      within(dialog).getByRole('button', { name: 'Revoke', exact: true }),
    );
    await verifyPassword();
    await screen.findByText(/token operation rejected/);
    expect(screen.getByText('CLI 7')).toBeVisible();
    expect(requests.filter(({ method }) => method === 'delete')).toHaveLength(
      phase === 'mutation' ? 1 : 0,
    );
  },
);

test('revokes a legacy token using the legacy context rather than a scoped token ID', async () => {
  accessTokens.legacy = { token_hint: 'old1', retire_at: 1900000000 };
  respond = ({ url, method }) => {
    if (url === '/api/user/access_tokens/legacy' && method === 'delete') {
      accessTokens = { ...accessTokens, legacy: null };
      return success();
    }
  };
  await openPersonalSettings();
  await securityTab();
  fireEvent.click(
    await screen.findByRole('button', { name: 'Revoke Legacy token' }),
  );
  const dialog = await screen.findByRole('dialog', { name: 'Revoke' });
  fireEvent.click(
    within(dialog).getByRole('button', { name: 'Revoke', exact: true }),
  );
  await verifyPassword();
  await waitFor(() =>
    expect(screen.queryByText('Legacy token')).not.toBeInTheDocument(),
  );
  expect(requests.find(({ url }) => url === '/api/verify').body).toMatchObject({
    scope: 'access_token.revoke',
    context: { legacy: true },
  });
});

test('a failed list load offers retry and never fabricates an empty token list', async () => {
  respond = ({ url, method }) =>
    url === '/api/user/access_tokens' && method === 'get'
      ? { success: false, message: 'token list unavailable' }
      : undefined;
  await openPersonalSettings();
  await securityTab();
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'token list unavailable',
  );
  expect(screen.queryByText('No access tokens')).not.toBeInTheDocument();
  respond = () => undefined;
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  expect(await screen.findByText('No access tokens')).toBeVisible();
});

test('cancelling an edited grant leaves the form intact and sends no PATCH', async () => {
  accessTokens.items = [managedToken()];
  await openPersonalSettings();
  await securityTab();
  const dialog = await openTokenEditor();
  fireEvent.click(
    within(dialog).getByRole('checkbox', { name: 'Profile / Edit' }),
  );
  fireEvent.click(
    within(dialog).getByRole('button', { name: 'Save', exact: true }),
  );
  await screen.findByPlaceholderText('请输入当前密码');
  const verificationDialog = screen
    .getByPlaceholderText('请输入当前密码')
    .closest('[role="dialog"]');
  fireEvent.click(
    within(verificationDialog).getByRole('button', { name: /取消|cancel/ }),
  );
  await waitFor(() =>
    expect(screen.getByLabelText('Token name')).toBeVisible(),
  );
  const restored = screen
    .getByLabelText('Token name')
    .closest('[role="dialog"]');
  expect(
    within(restored).getByRole('checkbox', { name: 'Profile / Edit' }),
  ).toBeChecked();
  expect(requests.some(({ method }) => method === 'patch')).toBe(false);
  fireEvent.click(
    within(restored).getByRole('button', { name: '取消', exact: true }),
  );
  expect(screen.getByText('CLI 7')).toBeVisible();
});

test('a failed name-only save preserves the editor and token without a false success', async () => {
  accessTokens.items = [managedToken()];
  respond = ({ method }) =>
    method === 'patch'
      ? { success: false, message: 'rename rejected' }
      : undefined;
  await openPersonalSettings();
  await securityTab();
  const dialog = await openTokenEditor();
  fireEvent.change(within(dialog).getByLabelText('Token name'), {
    target: { value: 'Rejected rename' },
  });
  fireEvent.click(
    within(dialog).getByRole('button', { name: 'Save', exact: true }),
  );
  await screen.findByText(/rename rejected/);
  const restored = await screen.findByRole('dialog', {
    name: 'Edit access token',
  });
  expect(within(restored).getByLabelText('Token name')).toHaveValue(
    'Rejected rename',
  );
  expect(screen.getByText('CLI 7')).toBeVisible();
  expect(requests.filter(({ method }) => method === 'patch')).toHaveLength(1);
});

test('removing all editable permissions disables save rather than sending an empty grant', async () => {
  accessTokens.items = [managedToken()];
  await openPersonalSettings();
  await securityTab();
  const dialog = await openTokenEditor();
  fireEvent.click(
    within(dialog).getByRole('checkbox', { name: 'Profile / View' }),
  );
  expect(
    within(dialog).getByRole('button', { name: 'Save', exact: true }),
  ).toBeDisabled();
  expect(requests.some(({ url }) => url === '/api/verify')).toBe(false);
  expect(requests.some(({ method }) => method === 'patch')).toBe(false);
});

test('a failed edit catalog load keeps the stored grant and sends no PATCH', async () => {
  accessTokens.items = [managedToken()];
  respond = ({ url }) =>
    url === '/api/user/access_tokens/catalog'
      ? { success: false, message: 'edit catalog unavailable' }
      : undefined;
  await openPersonalSettings();
  await securityTab();
  fireEvent.click(await screen.findByRole('button', { name: 'Edit CLI 7' }));
  await screen.findByText(/edit catalog unavailable/);
  expect(
    screen.queryByRole('dialog', { name: 'Edit access token' }),
  ).toBeNull();
  expect(screen.getByText('CLI 7')).toBeVisible();
  expect(requests.some(({ method }) => method === 'patch')).toBe(false);
});

test('unmount aborts an in-flight token mutation and does not replay it after a late response', async () => {
  accessTokens.items = [managedToken()];
  let release;
  respond = async ({ method }) => {
    if (method === 'delete') {
      await new Promise((resolve) => {
        release = resolve;
      });
      return success();
    }
  };
  const view = await openPersonalSettings();
  await securityTab();
  fireEvent.click(await screen.findByRole('button', { name: 'Revoke CLI 7' }));
  const dialog = await screen.findByRole('dialog', { name: 'Revoke' });
  fireEvent.click(
    within(dialog).getByRole('button', { name: 'Revoke', exact: true }),
  );
  await verifyPassword();
  await waitFor(() => expect(release).toBeTypeOf('function'));
  view.unmount();
  const request = requests.find(({ method }) => method === 'delete');
  expect(request.config.signal.aborted).toBe(true);
  await act(async () => {
    release();
  });
  expect(requests.filter(({ method }) => method === 'delete')).toHaveLength(1);
  expect(JSON.stringify(localStorage)).not.toContain('one-use-proof');
  expect(JSON.stringify(sessionStorage)).not.toContain('one-use-proof');
});

test('switching session aborts the old revoke and ignores its late response', async () => {
  accessTokens.items = [managedToken()];
  let release;
  respond = async ({ method }) => {
    if (method === 'delete') {
      await new Promise((resolve) => {
        release = resolve;
      });
      return success();
    }
  };
  const view = await openPersonalSettings();
  await securityTab();
  fireEvent.click(await screen.findByRole('button', { name: 'Revoke CLI 7' }));
  const dialog = await screen.findByRole('dialog', { name: 'Revoke' });
  fireEvent.click(
    within(dialog).getByRole('button', { name: 'Revoke', exact: true }),
  );
  await verifyPassword();
  await waitFor(() => expect(release).toBeTypeOf('function'));
  await act(async () => {
    accessTokens = { items: [], legacy: null };
    view.switchSession(
      { ...user, id: 10, username: 'next-user' },
      'next-session',
    );
  });
  expect(
    requests.find(({ method }) => method === 'delete').config.signal.aborted,
  ).toBe(true);
  expect(await screen.findByText('No access tokens')).toBeVisible();
  await act(async () => {
    release();
  });
  expect(screen.queryByText('CLI 7')).not.toBeInTheDocument();
  expect(getAuthState().session.sid).toBe('next-session');
  expect(requests.filter(({ method }) => method === 'delete')).toHaveLength(1);
});

test.each([undefined, 1, Number.NaN])(
  'rejects a Passkey verification flow with invalid expiry %s before requesting a credential',
  async (expires_at) => {
    respond = ({ url }) =>
      url === '/api/user/passkey/verify/begin'
        ? success({ flow_token: 'expired-flow', expires_at })
        : undefined;
    await expect(
      SecureVerificationService.verifyPasskey({
        scope: 'account.password.change',
      }),
    ).rejects.toThrow();
    expect(navigator.credentials.get).not.toHaveBeenCalled();
    expect(requests.map(({ url }) => url)).toEqual([
      '/api/user/passkey/verify/begin',
    ]);
  },
);

test('passkey enrollment sends proof only to begin, nests credential in the flow and accepts auth rotation', async () => {
  respond = ({ url }) => {
    if (url === '/api/user/passkey/register/begin')
      return success({
        flow_token: 'registration-flow',
        expires_at: Math.floor(Date.now() / 1000) + 60,
        options: {
          publicKey: {
            challenge: 'AQ',
            user: { id: 'AQ', name: 'personal-user', displayName: 'Personal' },
            rp: { name: 'new-api' },
            pubKeyCredParams: [],
          },
        },
      });
    if (url === '/api/user/passkey/register/finish') return success(rotation());
  };
  await openPersonalSettings();
  await securityTab();
  fireEvent.click(await screen.findByRole('button', { name: /注册 Passkey/ }));
  await verifyPassword();
  await waitFor(() =>
    expect(
      requests.some(({ url }) => url === '/api/user/passkey/register/finish'),
    ).toBe(true),
  );
  const begin = requests.find(
    ({ url }) => url === '/api/user/passkey/register/begin',
  );
  const finish = requests.find(
    ({ url }) => url === '/api/user/passkey/register/finish',
  );
  expect(begin.config.headers.get('X-Security-Proof')).toBe('one-use-proof');
  expect(begin.config.singleUseAuthorization).toBe(true);
  expect(finish.body).toMatchObject({
    flow_token: 'registration-flow',
    credential: { id: 'new-passkey', type: 'public-key' },
  });
  expect(finish.config.headers.has('X-Security-Proof')).toBe(false);
  expect(finish.config).toMatchObject({
    singleUseAuthorization: true,
    acceptAuthRotation: true,
  });
  await waitFor(() =>
    expect(getAuthState().access_token).toBe('rotated-session-token'),
  );
  expect(localStorage.getItem('user')).not.toContain('rotated-session-token');
});

test('password change uses the change scope and preserves the rotated session while refreshing the user', async () => {
  respond = ({ url, method: requestMethod }) =>
    url === '/api/user/self' && requestMethod === 'put'
      ? success({ ...rotation(), has_password: true })
      : undefined;
  await openPersonalSettings();
  await securityTab();
  fireEvent.click(screen.getByRole('button', { name: /修改密码/ }));
  fireEvent.change(await screen.findByPlaceholderText('请输入原密码'), {
    target: { value: 'current-password' },
  });
  fireEvent.change(screen.getByPlaceholderText('请输入新密码'), {
    target: { value: 'new-password-123' },
  });
  fireEvent.change(screen.getByPlaceholderText('请再次输入新密码'), {
    target: { value: 'new-password-123' },
  });
  confirmDialog();
  await verifyPassword();
  await waitFor(() =>
    expect(requests.some(({ method }) => method === 'put')).toBe(true),
  );
  const request = requests.find(({ method }) => method === 'put');
  expect(request.body).toEqual({
    original_password: 'current-password',
    password: 'new-password-123',
  });
  expect(request.config).toMatchObject({
    singleUseAuthorization: true,
    acceptAuthRotation: true,
  });
  expect(request.config.headers.get('X-Security-Proof')).toBe('one-use-proof');
  expect(requests.find(({ url }) => url === '/api/verify').body.scope).toBe(
    'account.password.change',
  );
  expect(getAuthState().session.sid).toBe('personal-session');
  expect(localStorage.getItem('user')).not.toContain('new-password-123');
});

test.each(['cancel', 'confirm'])(
  'opens and unmounts a real imperative modal via %s under React 19',
  async (action) => {
    const error = vi.spyOn(console, 'error');
    const onCancel = vi.fn();
    const onOk = vi.fn();
    await act(async () => {
      Modal.confirm({
        title: 'React 19 confirmation',
        content: 'Real imperative modal content',
        // jsdom has no CSS animation engine; use Semi's supported motion option.
        motion: false,
        onCancel,
        onOk,
      });
    });
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toBeVisible();
    expect(
      within(dialog).getByText('Real imperative modal content'),
    ).toBeVisible();
    fireEvent.click(
      within(dialog).getByRole('button', { name: action, exact: true }),
    );
    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(onCancel).toHaveBeenCalledTimes(action === 'cancel' ? 1 : 0);
    expect(onOk).toHaveBeenCalledTimes(action === 'confirm' ? 1 : 0);
    expect(
      error.mock.calls
        .flat()
        .some((value) => String(value).includes('createRoot is not available')),
    ).toBe(false);
  },
);

async function submitAccountDeletion() {
  await openPersonalSettings();
  await securityTab();
  fireEvent.click(screen.getByRole('button', { name: /删除账户/ }));
  fireEvent.change(
    await screen.findByPlaceholderText(/输入你的账户名.*以确认删除/),
    {
      target: { value: user.username },
    },
  );
  confirmDialog();
  await verifyPassword();
}

test.each(['success', 'rejected'])(
  'clears the deleted session in finally when the unified logout is %s',
  async (outcome) => {
    const logout = vi
      .spyOn(authentication, 'logoutAuthentication')
      .mockImplementation(async () => {
        if (outcome === 'rejected') throw new Error('session already revoked');
        clearAuthSession();
        return success();
      });
    respond = ({ url, method: requestMethod }) =>
      url === '/api/user/self' && requestMethod === 'delete'
        ? success()
        : undefined;
    await submitAccountDeletion();
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith('/login', { replace: true }),
    );
    expect(logout).toHaveBeenCalledOnce();
    expect(getStoredUser()).toBeNull();
    expect(getAuthState()).toBeNull();
    expect(
      requests
        .find(({ method }) => method === 'delete')
        .config.headers.get('X-Security-Proof'),
    ).toBe('one-use-proof');
    expect(requests.find(({ url }) => url === '/api/verify').body.scope).toBe(
      'account.delete',
    );
    expect(requests.some(({ url }) => url === '/api/user/auth/logout')).toBe(
      false,
    );
  },
);

test.each(['deletion', 'logout'])(
  'does not log out or clear a new session that appears during %s',
  async (phase) => {
    let release;
    let completed = false;
    const installNewSession = () =>
      persistAuthResponse({
        ...rotation(),
        access_token: 'new-session-token',
        user: { ...user, id: 10, username: 'new-user' },
        session: { sid: 'new-session', current: true },
      });
    const logout = vi
      .spyOn(authentication, 'logoutAuthentication')
      .mockImplementation(async () => {
        await new Promise((resolve) => {
          release = resolve;
        });
        completed = true;
        throw new Error('logout superseded');
      });
    respond = async ({ url, method: requestMethod }) => {
      if (url === '/api/user/self' && requestMethod === 'delete') {
        if (phase === 'deletion') {
          await new Promise((resolve) => {
            release = resolve;
          });
          completed = true;
        }
        return success();
      }
    };
    await submitAccountDeletion();
    await waitFor(() => expect(release).toBeTypeOf('function'));
    await act(async () => {
      installNewSession();
      release();
    });
    expect(completed).toBe(true);
    expect(logout).toHaveBeenCalledTimes(phase === 'logout' ? 1 : 0);
    expect(getAuthState().session.sid).toBe('new-session');
    expect(getStoredUser().username).toBe('new-user');
    expect(navigate).not.toHaveBeenCalled();
  },
);

test('does not log out or clear the session when account deletion fails', async () => {
  const logout = vi.spyOn(authentication, 'logoutAuthentication');
  respond = ({ url, method: requestMethod }) =>
    url === '/api/user/self' && requestMethod === 'delete'
      ? { success: false, message: 'deletion rejected' }
      : undefined;
  await submitAccountDeletion();
  await screen.findByText(/deletion rejected/);
  expect(logout).not.toHaveBeenCalled();
  expect(getAuthState().session.sid).toBe('personal-session');
  expect(navigate).not.toHaveBeenCalled();
});
