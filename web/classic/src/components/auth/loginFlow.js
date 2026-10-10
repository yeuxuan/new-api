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
import { t } from 'i18next';
import { API, updateAPI } from '../../helpers';
import { isAuthBundle, persistAuthResponse } from '../../helpers/authSession';
import {
  encryptPassword,
  clearPasswordEncryptionCache,
} from '../../helpers/passwordEncryption';
import {
  prepareCredentialRequestOptions,
  buildAssertionResult,
} from '../../helpers/passkey';

export function isLoginChallenge(value) {
  return (
    value?.require_verification === true &&
    typeof value.flow_token === 'string' &&
    value.flow_token.length > 0 &&
    Number.isFinite(value.expires_at) &&
    Array.isArray(value.methods) &&
    value.methods.length > 0 &&
    value.methods.every(
      (option) =>
        option &&
        ['2fa', 'passkey'].includes(option.method) &&
        typeof option.available === 'boolean',
    )
  );
}

export function completeLogin(bundle, dispatch) {
  if (!isAuthBundle(bundle)) throw new Error(t('登录失败，请重试'));
  const user = persistAuthResponse(bundle);
  dispatch({ type: 'login', payload: user });
  updateAPI();
  return user;
}

export async function loginWithPasskey(signal, challenge) {
  const options = {
    signal,
    skipAuthRefresh: true,
    skipErrorHandler: true,
    singleUseAuthorization: true,
  };
  const path = challenge
    ? '/api/user/login/passkey'
    : '/api/user/passkey/login';
  const begin = await API.post(
    `${path}/begin`,
    challenge ? { flow_token: challenge.flow_token } : undefined,
    options,
  );
  signal?.throwIfAborted();
  const data = begin.data?.data;
  if (
    !begin.data?.success ||
    !data?.flow_token ||
    !Number.isFinite(data.expires_at) ||
    data.expires_at * 1000 <= Date.now()
  ) {
    throw new Error(t('Passkey 登录失败，请重试'));
  }
  const assertion = await navigator.credentials.get({
    publicKey: prepareCredentialRequestOptions(data.options),
    signal,
  });
  signal?.throwIfAborted();
  if (!assertion || data.expires_at * 1000 <= Date.now()) {
    throw new Error(t('Passkey 验证失败，请重试'));
  }
  const finish = await API.post(
    `${path}/finish`,
    {
      ...(challenge
        ? {
            flow_token: challenge.flow_token,
            passkey_flow_token: data.flow_token,
          }
        : { flow_token: data.flow_token }),
      credential: buildAssertionResult(assertion),
    },
    options,
  );
  signal?.throwIfAborted();
  if (!finish.data?.success || !isAuthBundle(finish.data.data)) {
    throw new Error(t('Passkey 登录失败，请重试'));
  }
  return finish.data.data;
}

export async function verifyLoginChallenge(challenge, method, code, signal) {
  if (
    !isLoginChallenge(challenge) ||
    challenge.expires_at * 1000 <= Date.now() ||
    !challenge.methods.some((item) => item.method === method && item.available)
  ) {
    throw new Error(t('验证失败，请重试'));
  }
  signal?.throwIfAborted();
  if (method === 'passkey') return loginWithPasskey(signal, challenge);
  const trimmedCode = code.trim();
  if (!/^\d{6}$/.test(trimmedCode) && !/^[a-zA-Z0-9]{8}$/.test(trimmedCode)) {
    throw new Error(t('请输入6位验证码或8位备用码'));
  }
  const response = await API.post(
    '/api/user/login/verify',
    { flow_token: challenge.flow_token, method: '2fa', code: trimmedCode },
    {
      signal,
      skipAuthRefresh: true,
      skipErrorHandler: true,
      singleUseAuthorization: true,
    },
  );
  signal?.throwIfAborted();
  if (!response.data?.success || !isAuthBundle(response.data.data)) {
    throw new Error(t('验证失败，请重试'));
  }
  return response.data.data;
}

export async function loginWithPassword(
  username,
  password,
  encrypted,
  turnstile,
  signal,
) {
  const options = { signal, skipAuthRefresh: true, skipErrorHandler: true };
  try {
    const passwordFields = encrypted
      ? await encryptPassword(password)
      : { password };
    signal?.throwIfAborted();
    const response = await API.post(
      '/api/user/login',
      { username, ...passwordFields },
      {
        ...options,
        params: { turnstile },
      },
    );
    if (encrypted && !response.data?.success) clearPasswordEncryptionCache();
    return response;
  } catch (error) {
    if (encrypted) clearPasswordEncryptionCache();
    throw error;
  }
}
