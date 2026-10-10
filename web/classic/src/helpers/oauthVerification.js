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

import { API, logoutAuthentication } from './api';
import { applyAuthRotation, getStoredUser } from './authSession';
import { t } from 'i18next';

export const OAUTH_POPUP_CALLBACK_MESSAGE = 'oauth:popup:callback';
export const OAUTH_POPUP_RESULT_MESSAGE = 'oauth:popup:result';

const OAUTH_POPUP_FLOW_PREFIX = 'oauth_popup_flow:';
const POPUP_TIMEOUT = 10 * 60 * 1000;

function throwIfAborted(signal) {
  signal?.throwIfAborted();
}

function unwrap(response) {
  if (!response?.data?.success) {
    throw new Error(response?.data?.message || t('OAuth 授权失败'));
  }
  return response.data.data;
}

function callbackParams(callback) {
  return {
    state: callback.state,
    code: callback.code,
    error: callback.error,
    error_description: callback.errorDescription,
  };
}

function buildGitHubUrl(clientId, state) {
  const url = new URL('https://github.com/login/oauth/authorize');
  url.searchParams.set('client_id', clientId);
  url.searchParams.set('state', state);
  url.searchParams.set('scope', 'user:email');
  return url.toString();
}

function buildDiscordUrl(clientId, state) {
  const url = new URL('https://discord.com/oauth2/authorize');
  url.searchParams.set('client_id', clientId);
  url.searchParams.set(
    'redirect_uri',
    `${window.location.origin}/oauth/discord`,
  );
  url.searchParams.set('response_type', 'code');
  url.searchParams.set('scope', 'identify');
  url.searchParams.set('state', state);
  return url.toString();
}

function buildOIDCUrl(endpoint, clientId, state) {
  const url = new URL(endpoint);
  url.searchParams.set('client_id', clientId);
  url.searchParams.set('redirect_uri', `${window.location.origin}/oauth/oidc`);
  url.searchParams.set('response_type', 'code');
  url.searchParams.set('scope', 'openid profile email');
  url.searchParams.set('state', state);
  return url.toString();
}

function buildLinuxDOUrl(clientId, state) {
  const url = new URL('https://connect.linux.do/oauth2/authorize');
  url.searchParams.set('response_type', 'code');
  url.searchParams.set('client_id', clientId);
  url.searchParams.set('state', state);
  return url.toString();
}

function buildAuthorizationUrl(provider, state, status) {
  switch (provider) {
    case 'github':
      if (status.github_client_id) {
        return buildGitHubUrl(status.github_client_id, state);
      }
      break;
    case 'discord':
      if (status.discord_client_id) {
        return buildDiscordUrl(status.discord_client_id, state);
      }
      break;
    case 'oidc':
      if (status.oidc_authorization_endpoint && status.oidc_client_id) {
        return buildOIDCUrl(
          status.oidc_authorization_endpoint,
          status.oidc_client_id,
          state,
        );
      }
      break;
    case 'linuxdo':
      if (status.linuxdo_client_id) {
        return buildLinuxDOUrl(status.linuxdo_client_id, state);
      }
      break;
    default: {
      const custom = (status.custom_oauth_providers || []).find(
        (candidate) => candidate.slug === provider,
      );
      if (custom?.authorization_endpoint) {
        const url = new URL(custom.authorization_endpoint);
        url.searchParams.set('client_id', custom.client_id);
        url.searchParams.set(
          'redirect_uri',
          `${window.location.origin}/oauth/${provider}`,
        );
        url.searchParams.set('response_type', 'code');
        url.searchParams.set('state', state);
        if (custom.scopes) url.searchParams.set('scope', custom.scopes);
        return url.toString();
      }
    }
  }
  throw new Error(t('OAuth 提供商配置不可用'));
}

async function createOAuthAuthorization(
  provider,
  intent,
  operation,
  signal,
  proofToken,
) {
  throwIfAborted(signal);
  const response = await API.post(
    '/api/oauth/state',
    {
      provider,
      intent,
      ...(intent === 'login' && localStorage.getItem('aff')
        ? { aff: localStorage.getItem('aff') }
        : {}),
      scope: operation?.scope,
      ...(operation?.context ? { context: operation.context } : {}),
    },
    {
      skipAuthRefresh: intent === 'login',
      ...(proofToken ? { headers: { 'X-Security-Proof': proofToken } } : {}),
      singleUseAuthorization: intent === 'bind',
      skipErrorHandler: true,
      disableDuplicate: true,
      signal,
    },
  );
  const data = unwrap(response);
  throwIfAborted(signal);
  if (
    typeof data?.flow_token !== 'string' ||
    data.flow_token.length === 0 ||
    !Number.isFinite(data.expires_at) ||
    data.expires_at * 1000 <= Date.now()
  ) {
    throw new Error(t('验证流程已失效'));
  }
  return {
    state: data.flow_token,
    authorizationUrl: data.authorization_url,
    expiresAt: data.expires_at,
  };
}

function markPopup(popup, provider, state, intent) {
  try {
    const marker = JSON.stringify({ provider, state, intent });
    popup.sessionStorage.setItem(
      `${OAUTH_POPUP_FLOW_PREFIX}${provider}`,
      marker,
    );
    return (
      popup.sessionStorage.getItem(`${OAUTH_POPUP_FLOW_PREFIX}${provider}`) ===
      marker
    );
  } catch {
    return false;
  }
}

export function getOAuthCallbackMode(provider, state) {
  if (!provider || !state || !window.opener || window.opener.closed) {
    return 'login';
  }
  try {
    const raw = window.sessionStorage.getItem(
      `${OAUTH_POPUP_FLOW_PREFIX}${provider}`,
    );
    const marker = raw ? JSON.parse(raw) : null;
    if (
      marker?.provider !== provider ||
      marker?.state !== state ||
      !['bind', 'verify'].includes(marker.intent)
    ) {
      return 'login';
    }
    return marker.intent;
  } catch {
    return 'login';
  }
}

function watchPopupClosed(popup, onClosed) {
  const timer = window.setInterval(() => {
    if (popup.closed) {
      window.clearInterval(timer);
      onClosed();
    }
  }, 500);
  return () => window.clearInterval(timer);
}

export function openOAuthPopup({ provider, intent, signal, prepare }) {
  throwIfAborted(signal);
  const popup = window.open('', '_blank');
  if (!popup) throw new Error(t('OAuth 弹窗被浏览器拦截'));

  const controller = new AbortController();
  return new Promise((resolve, reject) => {
    let state = '';
    let settled = false;
    let received = false;
    let stopPopupWatcher = () => undefined;

    const cleanup = () => {
      window.removeEventListener('message', onMessage);
      signal?.removeEventListener('abort', onAbort);
      stopPopupWatcher();
      window.clearTimeout(deadline);
    };
    const fail = (error) => {
      if (settled) return;
      settled = true;
      const failure =
        error instanceof Error ? error : new Error(t('OAuth 授权失败'));
      controller.abort(failure);
      cleanup();
      if (!popup.closed) popup.close();
      reject(failure);
    };
    const onAbort = () =>
      fail(signal.reason || new Error(t('OAuth 授权已取消')));
    const finish = (result) => {
      if (settled) return;
      settled = true;
      cleanup();
      if (!popup.closed) {
        popup.postMessage(
          {
            type: OAUTH_POPUP_RESULT_MESSAGE,
            intent,
            provider,
            state,
            ...result,
          },
          window.location.origin,
        );
        popup.close();
      }
    };
    const onMessage = (event) => {
      if (
        received ||
        settled ||
        !state ||
        event.origin !== window.location.origin ||
        event.source !== popup
      ) {
        return;
      }
      const message = event.data;
      if (
        !message ||
        message.type !== OAUTH_POPUP_CALLBACK_MESSAGE ||
        message.intent !== intent ||
        message.provider !== provider ||
        message.state !== state ||
        (typeof message.code !== 'string' && typeof message.error !== 'string')
      ) {
        return;
      }
      received = true;
      stopPopupWatcher();
      window.clearTimeout(deadline);
      window.removeEventListener('message', onMessage);
      resolve({
        callback: {
          provider,
          state,
          code: message.code,
          error: message.error,
          errorDescription: message.errorDescription,
        },
        signal: controller.signal,
        finish,
      });
    };

    const deadline = window.setTimeout(
      () => fail(new Error(t('OAuth 授权超时，请重试'))),
      POPUP_TIMEOUT,
    );
    window.addEventListener('message', onMessage);
    signal?.addEventListener('abort', onAbort, { once: true });
    stopPopupWatcher = watchPopupClosed(popup, () =>
      fail(new Error(t('OAuth 授权已取消'))),
    );

    Promise.resolve()
      .then(() => prepare(controller.signal))
      .then((authorization) => {
        throwIfAborted(controller.signal);
        state = authorization.state;
        if (!markPopup(popup, provider, state, intent)) {
          throw new Error(t('OAuth 弹窗状态不可用'));
        }
        popup.location.replace(authorization.url);
      })
      .catch(fail);
  });
}

async function exchangeOAuthCallback(provider, exchange, binding = false) {
  try {
    const response = await API.get(
      `/api/oauth/${encodeURIComponent(provider)}`,
      {
        params: callbackParams(exchange.callback),
        singleUseAuthorization: true,
        skipErrorHandler: true,
        disableDuplicate: true,
        signal: exchange.signal,
      },
    );
    const data = unwrap(response);
    exchange.signal.throwIfAborted();
    if (binding && data?.action !== 'bind')
      throw new Error(t('OAuth 授权失败'));
    if (binding && data?.access_token !== undefined) applyAuthRotation(data);
    exchange.finish({ success: true });
    return data;
  } catch (error) {
    exchange.finish({
      success: false,
      message: error?.message || t('OAuth 授权失败'),
    });
    throw error;
  }
}

async function prepareOAuthFlow(
  provider,
  intent,
  operation,
  signal,
  proofToken,
) {
  const authorization = await createOAuthAuthorization(
    provider,
    intent,
    operation,
    signal,
    proofToken,
  );
  let url = authorization.authorizationUrl;
  if (!url) {
    const status = unwrap(
      await API.get('/api/status', {
        signal,
        skipErrorHandler: true,
        disableDuplicate: true,
        skipAuthRefresh: intent === 'login',
      }),
    );
    url = buildAuthorizationUrl(provider, authorization.state, status);
  }
  throwIfAborted(signal);
  if (!['https:', 'http:'].includes(new URL(url).protocol)) {
    throw new Error(t('授权失败'));
  }
  return {
    state: authorization.state,
    url,
    expiresAt: authorization.expiresAt,
  };
}

export async function prepareOAuthLogin(provider, signal) {
  throwIfAborted(signal);
  if (getStoredUser()) await logoutAuthentication();
  throwIfAborted(signal);
  return prepareOAuthFlow(provider, 'login', undefined, signal);
}

export async function verifyOAuth(provider, operation, signal) {
  const exchange = await openOAuthPopup({
    provider,
    intent: 'verify',
    signal,
    prepare: (popupSignal) =>
      prepareOAuthFlow(provider, 'verify', operation, popupSignal),
  });
  return exchangeOAuthCallback(provider, exchange);
}

export async function prepareOAuthBinding(provider, proofToken, signal) {
  if (!proofToken) throw new Error(t('OAuth 绑定需要安全验证'));
  return prepareOAuthFlow(provider, 'bind', undefined, signal, proofToken);
}

export async function bindOAuthAccount(
  provider,
  proofToken,
  signal,
  preparedAuthorization,
) {
  if (
    preparedAuthorization &&
    (!Number.isFinite(preparedAuthorization.expiresAt) ||
      preparedAuthorization.expiresAt * 1000 <= Date.now())
  ) {
    throw new Error(t('验证流程已失效'));
  }
  const exchange = await openOAuthPopup({
    provider,
    intent: 'bind',
    signal,
    prepare: (popupSignal) =>
      preparedAuthorization ||
      prepareOAuthBinding(provider, proofToken, popupSignal),
  });
  return exchangeOAuthCallback(provider, exchange, true);
}

export async function unbindOAuthAccount(providerId, proofToken, signal) {
  throwIfAborted(signal);
  if (!proofToken) throw new Error(t('验证失败，请重试'));
  const response = await API.delete(`/api/user/oauth/bindings/${providerId}`, {
    headers: { 'X-Security-Proof': proofToken },
    singleUseAuthorization: true,
    skipErrorHandler: true,
    signal,
  });
  const data = unwrap(response);
  throwIfAborted(signal);
  if (data?.access_token !== undefined) applyAuthRotation(data);
  return data;
}
