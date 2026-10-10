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

export async function loginWithPasskey(signal, challenge, selection = {}) {
  signal?.throwIfAborted();
  let remembered;
  if (!challenge) {
    try {
      const value = localStorage.getItem('passkey:last-successful-rp-id');
      if (value && value.length <= 253) remembered = value;
    } catch {
      // Optional domain hint; the server still validates every RP and ceremony.
    }
  }
  const options = {
    signal,
    skipAuthRefresh: true,
    skipErrorHandler: true,
    singleUseAuthorization: true,
  };
  const path = challenge
    ? '/api/user/login/passkey'
    : '/api/user/passkey/login';
  const rpID = selection.rpID ?? remembered;
  let beginPayload;
  if (challenge) beginPayload = { flow_token: challenge.flow_token };
  if (rpID) beginPayload = { ...beginPayload, rp_id: rpID };
  let begin = await API.post(`${path}/begin`, beginPayload, options);
  signal?.throwIfAborted();
  if (
    !challenge &&
    !selection.rpID &&
    remembered &&
    begin.data?.success === false &&
    begin.data?.code === 'PASSKEY_RP_ID_UNAVAILABLE'
  ) {
    try {
      localStorage.removeItem('passkey:last-successful-rp-id');
    } catch {
      // Storage is optional.
    }
    // Only retry a stale hint before a browser prompt, never an assertion/finish.
    begin = await API.post(`${path}/begin`, undefined, options);
    signal?.throwIfAborted();
  }
  const data = begin.data?.data;
  if (
    !begin.data?.success ||
    !data?.flow_token ||
    !Number.isFinite(data.expires_at) ||
    data.expires_at * 1000 <= Date.now()
  ) {
    throw new Error(t('Passkey 登录失败，请重试'));
  }
  const publicKey = prepareCredentialRequestOptions(data.options);
  let rpIDs = publicKey.rpId ? [publicKey.rpId] : [];
  if (Array.isArray(data.rp_ids)) {
    rpIDs = data.rp_ids.filter((id) => typeof id === 'string');
  }
  selection.onDomains?.({ rpID: publicKey.rpId, rpIDs });
  const assertion = await navigator.credentials.get({
    publicKey,
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
  if (!challenge && publicKey.rpId) {
    try {
      localStorage.setItem('passkey:last-successful-rp-id', publicKey.rpId);
    } catch {
      // A blocked preference store must not prevent successful login.
    }
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
