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
import { API } from '../helpers/api';
import { encryptPassword } from '../helpers/passwordEncryption';
import { verifyOAuth } from '../helpers/oauthVerification';
import {
  prepareCredentialRequestOptions,
  buildAssertionResult,
  isPasskeySupported,
} from '../helpers/passkey';

function operationFields(operation) {
  if (!operation?.scope) throw new Error(t('验证配置错误'));
  return {
    scope: operation.scope,
    ...(operation.context ? { context: operation.context } : {}),
  };
}

function unwrap(response) {
  const payload = response.data;
  if (!payload?.success) {
    throw new Error(payload?.message || t('验证失败'));
  }
  return payload.data;
}

function validateProof(proof, operation, method) {
  if (
    !proof?.proof_token ||
    proof.scope !== operation.scope ||
    proof.method !== method ||
    !Number.isFinite(proof.expires_at) ||
    proof.expires_at * 1000 <= Date.now()
  ) {
    throw new Error(t('服务端未返回有效的安全验证凭证'));
  }
  return proof;
}

/**
 * Classic 端安全验证适配器。所有敏感操作都必须显式携带 scoped proof。
 */
export class SecureVerificationService {
  static async checkAvailableVerificationMethods(scope, signal) {
    if (!scope) throw new Error(t('验证配置错误'));

    const [requirementsResponse, passkeySupported] = await Promise.all([
      API.get('/api/verify/methods', {
        params: { scope },
        disableDuplicate: true,
        skipErrorHandler: true,
        signal,
      }),
      isPasskeySupported(),
    ]);
    signal?.throwIfAborted();
    const requirements = unwrap(requirementsResponse);
    if (requirements?.scope !== scope || !Array.isArray(requirements.methods)) {
      throw new Error(t('验证配置错误'));
    }
    const methods = Array.isArray(requirements.methods)
      ? requirements.methods.map((option) => {
          if (
            option.method === 'passkey' &&
            option.available &&
            !passkeySupported
          ) {
            return { ...option, available: false };
          }
          return option;
        })
      : [];
    const available = new Set(
      methods
        .filter((option) => option.available)
        .map((option) => option.method),
    );

    return {
      scope,
      methods,
      has2FA: available.has('2fa'),
      hasPasskey: available.has('passkey'),
      hasPassword: available.has('password'),
      hasSession: available.has('session'),
      hasOAuth: available.has('oauth'),
      oauthProviders: requirements.oauth_providers || [],
      passkeySupported,
      passwordEncryptionEnabled:
        requirements.password_encryption_enabled === true,
    };
  }

  static async postVerification(method, operation, fields = {}, signal) {
    const response = await API.post(
      '/api/verify',
      {
        method,
        ...operationFields(operation),
        ...fields,
      },
      { signal, skipErrorHandler: true, singleUseAuthorization: true },
    );
    return unwrap(response);
  }

  static async verifyPasskey(operation, signal) {
    try {
      const beginResponse = await API.post(
        '/api/user/passkey/verify/begin',
        {
          ...operationFields(operation),
        },
        { signal, skipErrorHandler: true },
      );
      const begin = unwrap(beginResponse);
      if (
        !begin?.flow_token ||
        !Number.isFinite(begin.expires_at) ||
        begin.expires_at * 1000 <= Date.now()
      ) {
        throw new Error(t('验证流程已失效'));
      }

      const publicKey = prepareCredentialRequestOptions(begin.options || begin);
      const credential = await navigator.credentials.get({ publicKey, signal });
      signal?.throwIfAborted();
      if (!credential) throw new Error(t('Passkey 验证被取消'));

      const finishResponse = await API.post(
        '/api/user/passkey/verify/finish',
        {
          flow_token: begin.flow_token,
          credential: buildAssertionResult(credential),
        },
        { signal, singleUseAuthorization: true, skipErrorHandler: true },
      );
      return unwrap(finishResponse);
    } catch (error) {
      if (error?.name === 'NotAllowedError') {
        throw new Error(t('Passkey 验证被取消或超时'));
      }
      if (error?.name === 'InvalidStateError') {
        throw new Error(t('Passkey 验证状态无效'));
      }
      throw error;
    }
  }

  static async verify(
    method,
    operation,
    code = '',
    password = '',
    options = {},
  ) {
    const { signal, passwordEncryptionEnabled, provider } = options;
    let proof;
    switch (method) {
      case '2fa':
        if (!code?.trim()) throw new Error(t('请输入验证码或备用码'));
        proof = await this.postVerification(
          '2fa',
          operation,
          {
            code: code.trim(),
          },
          signal,
        );
        break;
      case 'password': {
        if (!password?.trim()) throw new Error(t('请输入当前密码'));
        const fields = passwordEncryptionEnabled
          ? await encryptPassword(password)
          : { password };
        signal?.throwIfAborted();
        proof = await this.postVerification(
          'password',
          operation,
          fields,
          signal,
        );
        break;
      }
      case 'session':
        proof = await this.postVerification('session', operation, {}, signal);
        break;
      case 'passkey':
        proof = await this.verifyPasskey(operation, signal);
        break;
      case 'oauth':
        proof = await verifyOAuth(provider, operation, signal);
        break;
      default:
        throw new Error(t('验证配置错误'));
    }
    signal?.throwIfAborted();
    return validateProof(proof, operation, method);
  }
}

export const createApiCalls = {
  viewChannelKey: (channelId) => async (proofToken, signal) => {
    if (!proofToken) throw new Error(t('验证配置错误'));
    const response = await API.post(
      `/api/channel/${channelId}/key`,
      undefined,
      {
        headers: { 'X-Security-Proof': proofToken },
        singleUseAuthorization: true,
        signal,
      },
    );
    return response.data;
  },

  custom:
    (url, method = 'POST', extraData = {}) =>
    async (proofToken, signal) => {
      if (!proofToken) throw new Error(t('验证配置错误'));
      const config = {
        headers: { 'X-Security-Proof': proofToken },
        singleUseAuthorization: true,
        signal,
      };
      let response;
      switch (method.toUpperCase()) {
        case 'GET':
          response = await API.get(url, { ...config, params: extraData });
          break;
        case 'POST':
          response = await API.post(url, extraData, config);
          break;
        case 'PUT':
          response = await API.put(url, extraData, config);
          break;
        case 'DELETE':
          response = await API.delete(url, { ...config, data: extraData });
          break;
        default:
          throw new Error(`不支持的HTTP方法: ${method}`);
      }
      return response.data;
    },
};
