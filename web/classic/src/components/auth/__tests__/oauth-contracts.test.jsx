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

import React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react';
import LoginForm from '../LoginForm';
import RegisterForm from '../RegisterForm';
import AccountManagement from '../../settings/personal/cards/AccountManagement';
import { UserContext } from '../../../context/User';
import { StatusContext } from '../../../context/Status';
import { API, logoutAuthentication } from '../../../helpers/api';
import { showError, showSuccess, showWarning } from '../../../helpers';
import {
  clearAuthSession,
  getAuthState,
  persistAuthResponse,
} from '../../../helpers/authSession';
import {
  getOAuthCallbackMode,
  openOAuthPopup,
  OAUTH_POPUP_CALLBACK_MESSAGE,
  prepareOAuthLogin,
  prepareOAuthBinding,
  bindOAuthAccount,
} from '../../../helpers/oauthVerification';

vi.mock('react-router-dom', () => ({
  useNavigate: () => vi.fn(),
  useSearchParams: () => [new URLSearchParams()],
  Link: ({ to, children, ...props }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
}));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: translate, i18n: { language: 'zh-CN' } }),
}));
vi.mock('i18next', async (importOriginal) => ({
  ...(await importOriginal()),
  t: (key) => key,
}));
vi.mock('../../../helpers', async (importOriginal) => ({
  ...(await importOriginal()),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showWarning: vi.fn(),
  showInfo: vi.fn(),
}));
vi.mock('../../../helpers/api', async (importOriginal) => ({
  ...(await importOriginal()),
  logoutAuthentication: vi.fn(),
}));

function translate(key, values) {
  return key.replace(/\{\{(\w+)\}\}/g, (_, name) => values?.[name] ?? '');
}

const status = {
  github_oauth: true,
  discord_oauth: true,
  oidc_enabled: true,
  linuxdo_oauth: true,
  telegram_oauth: true,
  custom_oauth_providers: [{ id: 6, slug: 'work', name: 'Work' }],
};
const providers = [
  ['github', 'GitHub'],
  ['discord', 'Discord'],
  ['oidc', 'OIDC'],
  ['linuxdo', 'LinuxDO'],
  ['telegram', 'Telegram'],
  ['work', 'Work'],
];
const user = { id: 9, username: 'oauth-user', role: 1, has_password: true };
const rotation = (token = 'oauth-session-token') => ({
  access_token: token,
  token_type: 'Bearer',
  access_expires_at: 4102444800,
  session: { sid: 'oauth-session', current: true },
});
const success = (data) => ({ success: true, data });
let originalAdapter;
let requests;
let respond;
let bindings;

function renderEntry(Component) {
  return render(
    <StatusContext.Provider value={[{ status }]}>
      <UserContext.Provider value={[{ user: null }, vi.fn()]}>
        <Component />
      </UserContext.Provider>
    </StatusContext.Provider>,
  );
}

function renderBindings(onUpdate = vi.fn()) {
  persistAuthResponse({ user, ...rotation() });
  return render(
    <AccountManagement
      t={translate}
      userState={{ user: getAuthState().user }}
      status={status}
      onUpdate={onUpdate}
    />,
  );
}

function bindingButton(name, label = '绑定') {
  const card = screen.getByText(name, { exact: true }).closest('.semi-card');
  return within(card).getByRole('button', { name: label, exact: true });
}

async function verifyPassword() {
  fireEvent.change(await screen.findByPlaceholderText('请输入当前密码'), {
    target: { value: 'current-password' },
  });
  fireEvent.click(screen.getByRole('button', { name: '验证', exact: true }));
}

function popupWindow() {
  const popup = {
    closed: false,
    location: { replace: vi.fn() },
    sessionStorage: storage(),
    postMessage: vi.fn(),
    close: vi.fn(() => {
      popup.closed = true;
    }),
  };
  vi.spyOn(window, 'open').mockReturnValue(popup);
  return popup;
}

function storage() {
  const values = new Map();
  return {
    getItem: (key) => values.get(key) || null,
    setItem: (key, value) => values.set(key, value),
  };
}

function message(data, source, origin = window.location.origin) {
  const event = new MessageEvent('message', { data, origin });
  Object.defineProperty(event, 'source', { configurable: true, value: source });
  window.dispatchEvent(event);
}

beforeEach(() => {
  requests = [];
  bindings = [];
  respond = () => undefined;
  localStorage.clear();
  clearAuthSession();
  window.history.replaceState({}, '', '/');
  window.sessionStorage.clear();
  logoutAuthentication.mockImplementation(async () => clearAuthSession());
  originalAdapter = API.defaults.adapter;
  API.defaults.adapter = async (config) => {
    const body = config.data ? JSON.parse(config.data) : undefined;
    const request = { url: config.url, method: config.method, body, config };
    requests.push(request);
    let data = await respond(request);
    if (!data) {
      switch (config.url) {
        case '/api/oauth/state':
          data = success({
            flow_token: 'state-1',
            expires_at: 4102444800,
            authorization_url: `${window.location.origin}/#authorized-${body.provider}`,
          });
          break;
        case '/api/status':
          data = success(status);
          break;
        case '/api/user/oauth/bindings':
          data = success(bindings);
          break;
        case '/api/user/2fa/status':
          data = success({ enabled: false });
          break;
        case '/api/verify/methods':
          data = success({
            scope: config.params.scope,
            methods: [{ method: 'password', available: true }],
          });
          break;
        case '/api/verify':
          data = success({
            proof_token: 'one-use-proof',
            scope: body.scope,
            method: body.method,
            expires_at: 4102444800,
          });
          break;
        default:
          throw new Error(
            `Unexpected OAuth request: ${config.method} ${config.url}`,
          );
      }
    }
    return { data, config, status: 200, statusText: 'OK', headers: {} };
  };
  Object.defineProperty(window, 'opener', {
    configurable: true,
    value: null,
  });
});

afterEach(() => {
  cleanup();
  API.defaults.adapter = originalAdapter;
  clearAuthSession();
});

describe.each([
  ['login', LoginForm],
  ['register', RegisterForm],
])('Classic %s OAuth entry', (_, Component) => {
  it.each(providers)(
    'starts %s from the actual button using POST and server authorization URL',
    async (provider, name) => {
      localStorage.setItem('aff', 'referral');
      renderEntry(Component);
      fireEvent.click(
        screen.getByRole('button', { name: new RegExp(`使用 ${name} 继续`) }),
      );
      await waitFor(() =>
        expect(window.location.hash).toBe(`#authorized-${provider}`),
      );
      const calls = requests.filter(
        (request) => request.url === '/api/oauth/state',
      );
      expect(calls).toHaveLength(1);
      expect(calls[0]).toMatchObject({
        method: 'post',
        body: { provider, intent: 'login', aff: 'referral' },
      });
      expect(calls[0].config.skipAuthRefresh).toBe(true);
      expect(
        requests.some((request) => request.url.includes('/telegram/login')),
      ).toBe(false);
      expect(localStorage.getItem('user')).toBeNull();
      expect(JSON.stringify(sessionStorage)).not.toContain('state-1');
    },
  );

  it('shows server rejection and allows another attempt', async () => {
    respond = ({ url }) =>
      url === '/api/oauth/state'
        ? { success: false, message: 'provider unavailable' }
        : undefined;
    renderEntry(Component);
    const button = screen.getByRole('button', { name: /使用 GitHub 继续/ });
    fireEvent.click(button);
    await waitFor(() =>
      expect(showError).toHaveBeenCalledWith('provider unavailable'),
    );
    expect(button).not.toBeDisabled();
    expect(window.location.hash).toBe('');
  });

  it('cancels an in-flight authorization when the entry unmounts', async () => {
    let release;
    respond = ({ url }) =>
      url === '/api/oauth/state'
        ? new Promise((resolve) => {
            release = resolve;
          })
        : undefined;
    const entry = renderEntry(Component);
    fireEvent.click(screen.getByRole('button', { name: /使用 GitHub 继续/ }));
    await waitFor(() => expect(release).toBeTypeOf('function'));
    const request = requests.find((item) => item.url === '/api/oauth/state');
    entry.unmount();
    expect(request.config.signal.aborted).toBe(true);
    release(
      success({
        flow_token: 'state-1',
        expires_at: 4102444800,
        authorization_url: `${window.location.origin}/#late`,
      }),
    );
    await waitFor(() => expect(showError).not.toHaveBeenCalled());
    expect(window.location.hash).toBe('');
  });
});

describe('Classic account OAuth entry contracts', () => {
  it.each(providers)(
    'verifies the %s binding context before preparing and completing the popup',
    async (provider, name) => {
      const onUpdate = vi.fn();
      respond = ({ url }) =>
        url === `/api/oauth/${provider}`
          ? success({
              action: 'bind',
              ...(provider === 'github'
                ? {}
                : rotation('binding-rotated-token')),
            })
          : undefined;
      const popup = popupWindow();
      renderBindings(onUpdate);
      fireEvent.click(bindingButton(name));
      await verifyPassword();
      await screen.findByText('验证成功！');
      expect(window.open).not.toHaveBeenCalled();
      expect(
        requests.find((request) => request.url === '/api/verify').body,
      ).toMatchObject({
        scope: 'account.binding.bind',
        context: { provider },
      });
      const state = requests.find(
        (request) => request.url === '/api/oauth/state',
      );
      expect(state.body).toEqual({ provider, intent: 'bind' });
      expect(state.config.headers.get('X-Security-Proof')).toBe(
        'one-use-proof',
      );
      expect(state.config.singleUseAuthorization).toBe(true);
      fireEvent.click(
        screen.getByRole('button', { name: 'confirm', exact: true }),
      );
      await waitFor(() => expect(popup.location.replace).toHaveBeenCalled());
      message(
        {
          type: OAUTH_POPUP_CALLBACK_MESSAGE,
          provider,
          intent: 'bind',
          state: 'state-1',
          code: 'oauth-code',
        },
        popup,
      );
      await waitFor(() => expect(onUpdate).toHaveBeenCalledTimes(1));
      expect(getAuthState().access_token).toBe(
        provider === 'github' ? 'oauth-session-token' : 'binding-rotated-token',
      );
      expect(JSON.stringify(localStorage)).not.toContain(
        'binding-rotated-token',
      );
      expect(
        requests.filter((request) => request.url === '/api/oauth/state'),
      ).toHaveLength(1);
      expect(
        requests.filter((request) => request.url === `/api/oauth/${provider}`),
      ).toHaveLength(1);
    },
  );

  it('does not prepare binding after verification is cancelled', async () => {
    renderBindings();
    fireEvent.click(bindingButton('GitHub'));
    await screen.findByPlaceholderText('请输入当前密码');
    fireEvent.click(screen.getByRole('button', { name: '取消', exact: true }));
    expect(requests.some((request) => request.url === '/api/oauth/state')).toBe(
      false,
    );
    expect(bindingButton('GitHub')).not.toBeDisabled();
  });

  it('discards a prepared binding when the continuation is cancelled', async () => {
    const popup = popupWindow();
    renderBindings();
    fireEvent.click(bindingButton('GitHub'));
    await verifyPassword();
    await screen.findByText('验证成功！');
    fireEvent.click(
      screen.getByRole('button', { name: 'cancel', exact: true }),
    );
    expect(popup.location.replace).not.toHaveBeenCalled();
    expect(bindingButton('GitHub')).not.toBeDisabled();
    expect(
      requests.some((request) => request.url === '/api/oauth/github'),
    ).toBe(false);
  });

  it.each([
    ['rejected callback', { success: false, message: 'binding rejected' }],
    [
      'login result instead of binding',
      success({ user, ...rotation('wrong-token') }),
    ],
    [
      'rotation for a different session',
      success({
        action: 'bind',
        ...rotation('wrong-token'),
        session: { sid: 'other-session', current: true },
      }),
    ],
  ])('does not replay or accept a %s', async (_, response) => {
    const onUpdate = vi.fn();
    const popup = popupWindow();
    respond = ({ url }) => (url === '/api/oauth/github' ? response : undefined);
    renderBindings(onUpdate);
    fireEvent.click(bindingButton('GitHub'));
    await verifyPassword();
    await screen.findByText('验证成功！');
    fireEvent.click(
      screen.getByRole('button', { name: 'confirm', exact: true }),
    );
    await waitFor(() => expect(popup.location.replace).toHaveBeenCalled());
    const callback = {
      type: OAUTH_POPUP_CALLBACK_MESSAGE,
      provider: 'github',
      intent: 'bind',
      state: 'state-1',
      code: 'oauth-code',
    };
    message(callback, popup);
    message(callback, popup);
    await waitFor(() => expect(showError).toHaveBeenCalled());
    expect(
      requests.filter((request) => request.url === '/api/oauth/github'),
    ).toHaveLength(1);
    expect(onUpdate).not.toHaveBeenCalled();
    expect(showSuccess).not.toHaveBeenCalled();
    expect(getAuthState().access_token).toBe('oauth-session-token');
    expect(popup.close).toHaveBeenCalledOnce();
  });

  it('requires a scoped proof for custom account unbinding and updates the session in memory', async () => {
    bindings = [{ provider_id: 6, provider_user_id: 'work-user' }];
    const onUpdate = vi.fn();
    respond = ({ url }) =>
      url === '/api/user/oauth/bindings/6'
        ? success(rotation('unbind-rotated-token'))
        : undefined;
    renderBindings(onUpdate);
    await waitFor(() => expect(bindingButton('Work', '解绑')).toBeEnabled());
    fireEvent.click(bindingButton('Work', '解绑'));
    fireEvent.click(
      await screen.findByRole('button', { name: 'confirm', exact: true }),
    );
    await verifyPassword();
    await waitFor(() => expect(onUpdate).toHaveBeenCalledTimes(1));
    expect(
      requests.find((request) => request.url === '/api/verify').body,
    ).toMatchObject({
      scope: 'account.binding.unbind',
      context: { provider_id: 6 },
    });
    const removal = requests.find((request) => request.method === 'delete');
    expect(removal.url).toBe('/api/user/oauth/bindings/6');
    expect(removal.config.headers.get('X-Security-Proof')).toBe(
      'one-use-proof',
    );
    expect(removal.config.singleUseAuthorization).toBe(true);
    expect(getAuthState().access_token).toBe('unbind-rotated-token');
    expect(JSON.stringify(localStorage)).not.toContain('unbind-rotated-token');
  });

  it('does not replay a rejected unbind proof or update user metadata', async () => {
    bindings = [{ provider_id: 6, provider_user_id: 'work-user' }];
    const onUpdate = vi.fn();
    respond = ({ url }) =>
      url === '/api/user/oauth/bindings/6'
        ? { success: false, message: 'proof expired' }
        : undefined;
    renderBindings(onUpdate);
    await waitFor(() => expect(bindingButton('Work', '解绑')).toBeEnabled());
    fireEvent.click(bindingButton('Work', '解绑'));
    fireEvent.click(
      screen.getByRole('button', { name: 'confirm', exact: true }),
    );
    await verifyPassword();
    await waitFor(() =>
      expect(showError).toHaveBeenCalledWith('proof expired'),
    );
    expect(
      requests.filter((request) => request.method === 'delete'),
    ).toHaveLength(1);
    expect(onUpdate).not.toHaveBeenCalled();
    expect(showSuccess).not.toHaveBeenCalled();
  });

  it('retains a successful unbind when only the security notification fails', async () => {
    bindings = [{ provider_id: 6, provider_user_id: 'work-user' }];
    const onUpdate = vi.fn();
    respond = ({ url }) =>
      url === '/api/user/oauth/bindings/6'
        ? success({ notification_warning: true })
        : undefined;
    renderBindings(onUpdate);
    await waitFor(() => expect(bindingButton('Work', '解绑')).toBeEnabled());
    fireEvent.click(bindingButton('Work', '解绑'));
    fireEvent.click(
      screen.getByRole('button', { name: 'confirm', exact: true }),
    );
    await verifyPassword();
    await waitFor(() => expect(onUpdate).toHaveBeenCalledOnce());
    expect(showWarning).toHaveBeenCalledWith(
      'The change succeeded, but the notification email could not be sent.',
    );
    expect(showSuccess).toHaveBeenCalledWith('解绑成功');
    expect(getAuthState().access_token).toBe('oauth-session-token');
  });
});

describe('Classic OAuth authorization contracts', () => {
  it('logs out an existing session through the current logout helper', async () => {
    persistAuthResponse({ user, ...rotation() });
    await prepareOAuthLogin('github');
    expect(logoutAuthentication).toHaveBeenCalledOnce();
    expect(getAuthState()).toBeNull();
  });

  it('does not start OAuth when logout fails', async () => {
    persistAuthResponse({ user, ...rotation() });
    logoutAuthentication.mockRejectedValueOnce(new Error('logout rejected'));
    await expect(prepareOAuthLogin('github')).rejects.toThrow(
      'logout rejected',
    );
    expect(requests).toHaveLength(0);
  });

  it('supports providers whose POST response has no authorization URL', async () => {
    respond = ({ url }) => {
      if (url === '/api/oauth/state')
        return success({ flow_token: 'state-1', expires_at: 4102444800 });
      if (url === '/api/status')
        return success({ github_client_id: 'github-client' });
    };
    const authorization = await prepareOAuthLogin('github');
    expect(new URL(authorization.url).searchParams.get('state')).toBe(
      'state-1',
    );
    expect(authorization.url).toContain(
      'https://github.com/login/oauth/authorize',
    );
  });

  it('rejects expired flows and binding without a security proof', async () => {
    respond = () => success({ flow_token: 'expired', expires_at: 1 });
    await expect(prepareOAuthLogin('github')).rejects.toThrow();
    requests = [];
    await expect(prepareOAuthBinding('github')).rejects.toThrow();
    await expect(
      bindOAuthAccount('github', undefined, undefined, {
        state: 'expired',
        expiresAt: 1,
      }),
    ).rejects.toThrow();
    expect(requests).toHaveLength(0);
    expect(showSuccess).not.toHaveBeenCalled();
  });

  it('uses the Discord identify scope without encoding a literal plus as a scope', async () => {
    respond = ({ url }) => {
      if (url === '/api/oauth/state')
        return success({ flow_token: 'state-1', expires_at: 4102444800 });
      if (url === '/api/status')
        return success({ discord_client_id: 'discord-client' });
    };
    const authorization = await prepareOAuthLogin('discord');
    expect(new URL(authorization.url).searchParams.get('scope')).toBe(
      'identify',
    );
    expect(new URL(authorization.url).searchParams.get('redirect_uri')).toBe(
      `${window.location.origin}/oauth/discord`,
    );
  });
});

describe('Classic OAuth popup contracts', () => {
  it('accepts callback mode only for the exact provider and state', () => {
    Object.defineProperty(window, 'opener', {
      configurable: true,
      value: { closed: false },
    });
    window.sessionStorage.setItem(
      'oauth_popup_flow:github',
      JSON.stringify({
        provider: 'github',
        state: 'state-1',
        intent: 'verify',
      }),
    );

    expect(getOAuthCallbackMode('github', 'state-1')).toBe('verify');
    expect(getOAuthCallbackMode('github', 'state-2')).toBe('login');
    expect(getOAuthCallbackMode('discord', 'state-1')).toBe('login');
  });

  it('requires same origin and the exact popup source before accepting a callback', async () => {
    const popup = {
      closed: false,
      location: { replace: vi.fn() },
      sessionStorage: storage(),
      postMessage: vi.fn(),
      close: vi.fn(() => {
        popup.closed = true;
      }),
    };
    vi.spyOn(window, 'open').mockReturnValue(popup);

    const exchangePromise = openOAuthPopup({
      provider: 'github',
      intent: 'verify',
      signal: new AbortController().signal,
      prepare: vi.fn().mockResolvedValue({
        state: 'state-1',
        url: 'https://github.com/login/oauth/authorize',
      }),
    });
    await vi.waitFor(() => expect(popup.location.replace).toHaveBeenCalled());

    const callback = {
      type: OAUTH_POPUP_CALLBACK_MESSAGE,
      intent: 'verify',
      provider: 'github',
      state: 'state-1',
      code: 'oauth-code',
    };
    message(callback, {}, window.location.origin);
    message(callback, popup, 'https://attacker.example');
    message(callback, popup);

    const exchange = await exchangePromise;
    expect(exchange.callback.code).toBe('oauth-code');
    exchange.finish({ success: true });
    expect(popup.postMessage).toHaveBeenCalledWith(
      expect.objectContaining({ success: true, state: 'state-1' }),
      window.location.origin,
    );
  });
});
