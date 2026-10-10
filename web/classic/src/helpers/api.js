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

import { showError, formatMessageForAPI, isValidMessage } from './utils';
import axios from 'axios';
import { t } from 'i18next';
import { MESSAGE_ROLES } from '../constants/playground.constants';
import {
  applyAuthRotation,
  clearAuthSession,
  getAuthRevision,
  getAuthState,
  getStoredUser,
  isAuthBundle,
  persistAuthResponse,
} from './authSession';

const baseURL = import.meta.env.VITE_REACT_APP_SERVER_URL || '';
const refreshClient = axios.create({
  baseURL,
  withCredentials: true,
  headers: { 'Cache-Control': 'no-cache, no-store' },
});
let refreshPromise = null;

export function refreshAuthentication() {
  if (refreshPromise) return refreshPromise;
  const revision = getAuthRevision();
  const storedUser = getStoredUser();
  const refresh = async () => {
    const response = await refreshClient.post(
      '/api/user/auth/refresh',
      undefined,
      {
        headers: storedUser?.session?.sid
          ? { 'X-Auth-Session': storedUser.session.sid }
          : undefined,
      },
    );
    if (
      getAuthRevision() !== revision ||
      getStoredUser()?.id !== storedUser?.id ||
      getStoredUser()?.session?.sid !== storedUser?.session?.sid
    ) {
      throw new Error('Authentication refresh was superseded');
    }
    const bundle = response.data?.data;
    if (
      !response.data?.success ||
      !isAuthBundle(bundle) ||
      (storedUser?.id && bundle.user.id !== storedUser.id) ||
      (storedUser?.session?.sid &&
        bundle.session.sid !== storedUser.session.sid)
    ) {
      throw new Error('Invalid authentication refresh response');
    }
    persistAuthResponse(bundle);
    return bundle;
  };
  // Serialize refresh-cookie rotation with other tabs, without persisting tokens.
  refreshPromise = Promise.resolve()
    .then(() =>
      typeof navigator !== 'undefined' && navigator.locks
        ? navigator.locks.request('new-api:auth-refresh', refresh)
        : refresh(),
    )
    .finally(() => {
      refreshPromise = null;
    });
  return refreshPromise;
}

export async function getFreshAuthHeaders() {
  let auth = getAuthState();
  if (!auth || auth.access_expires_at <= Date.now() / 1000 + 60) {
    auth = await refreshAuthentication();
  }
  return {
    'Content-Type': 'application/json',
    'New-API-User': String(auth.user.id),
    Authorization: `Bearer ${auth.access_token}`,
  };
}

export async function logoutAuthentication() {
  const sid = getStoredUser()?.session?.sid;
  const auth = getAuthState();
  const response = await refreshClient.post(
    '/api/user/auth/logout',
    undefined,
    {
      headers: {
        ...(sid ? { 'X-Auth-Session': sid } : {}),
        ...(auth ? { Authorization: `Bearer ${auth.access_token}` } : {}),
      },
    },
  );
  if (!response.data?.success) {
    throw new Error(response.data?.message || t('操作失败，请重试'));
  }
  if (getStoredUser()?.session?.sid !== sid) {
    throw new Error(t('操作失败，请重试'));
  }
  clearAuthSession();
  return response.data;
}

function createAPI() {
  const instance = axios.create({
    baseURL,
    withCredentials: true,
    headers: {
      'Cache-Control': 'no-cache, no-store',
    },
  });

  instance.interceptors.request.use(async (config) => {
    const user = getStoredUser();
    let auth = getAuthState();
    const hasSecurityProof =
      config.headers?.has?.('X-Security-Proof') ||
      config.headers?.['X-Security-Proof'];
    const singleUse = Boolean(
      config.singleUseAuthorization || hasSecurityProof,
    );
    if (
      !config.skipAuthRefresh &&
      user &&
      (!auth || auth.access_expires_at <= Date.now() / 1000 + 60)
    ) {
      auth = await refreshAuthentication();
    }
    if (singleUse) config.skipAuthRefresh = true;
    config.headers = config.headers || {};
    if (user?.id) config.headers['New-API-User'] = user.id;
    if (auth) config.headers.Authorization = `Bearer ${auth.access_token}`;
    return config;
  });

  instance.interceptors.response.use(
    (response) => {
      // Payment responses use message as a status and put their text in data.
      for (const body of [response.data, response.data?.data]) {
        if (typeof body?.message_key !== 'string' || !body.message_key)
          continue;
        const params = body.message_params;
        const message = t(
          body.message_key,
          params && typeof params === 'object' && !Array.isArray(params)
            ? params
            : {},
        );
        if (body.message === 'error' || body.message === 'success') {
          if (typeof body.data === 'string') body.data = message;
        } else {
          body.message = message;
        }
      }
      if (
        response.config?.acceptAuthRotation &&
        response.data?.success === true
      ) {
        applyAuthRotation(response.data?.data);
      }
      if (
        response.config?.url === '/api/user/auth/logout' &&
        response.data?.success
      ) {
        clearAuthSession();
      }
      return response;
    },
    async (error) => {
      const config = error.config;
      if (
        error.response?.status === 401 &&
        config &&
        !config.skipAuthRefresh &&
        !config.authRetry &&
        getStoredUser()
      ) {
        config.authRetry = true;
        try {
          await refreshAuthentication();
          return instance.request(config);
        } catch (refreshError) {
          if (refreshError.response?.status === 401) clearAuthSession();
          return Promise.reject(refreshError);
        }
      }
      if (error.config && error.config.skipErrorHandler) {
        return Promise.reject(error);
      }
      showError(error);
      return Promise.reject(error);
    },
  );

  patchAPIInstance(instance);
  return instance;
}

export let API = createAPI();
function patchAPIInstance(instance) {
  const originalGet = instance.get.bind(instance);
  const inFlightGetRequests = new Map();

  const genKey = (url, config = {}) => {
    const params = config.params ? JSON.stringify(config.params) : '{}';
    const user = getStoredUser();
    return `${user?.session?.sid || user?.id || 'anonymous'}:${url}?${params}`;
  };

  instance.get = (url, config = {}) => {
    if (
      config?.disableDuplicate ||
      config.singleUseAuthorization ||
      config.headers?.['X-Security-Proof']
    ) {
      return originalGet(url, config);
    }

    const key = genKey(url, config);
    if (inFlightGetRequests.has(key)) {
      return inFlightGetRequests.get(key);
    }

    const reqPromise = originalGet(url, config).finally(() => {
      inFlightGetRequests.delete(key);
    });

    inFlightGetRequests.set(key, reqPromise);
    return reqPromise;
  };
}

export function updateAPI() {
  API = createAPI();
}

// playground

// 构建API请求负载
export const buildApiPayload = (
  messages,
  systemPrompt,
  inputs,
  parameterEnabled,
) => {
  const processedMessages = messages
    .filter(isValidMessage)
    .map(formatMessageForAPI)
    .filter(Boolean);

  // 如果有系统提示，插入到消息开头
  if (systemPrompt && systemPrompt.trim()) {
    processedMessages.unshift({
      role: MESSAGE_ROLES.SYSTEM,
      content: systemPrompt.trim(),
    });
  }

  const payload = {
    model: inputs.model,
    group: inputs.group,
    messages: processedMessages,
    stream: inputs.stream,
  };

  // 添加启用的参数
  const parameterMappings = {
    temperature: 'temperature',
    top_p: 'top_p',
    max_tokens: 'max_tokens',
    frequency_penalty: 'frequency_penalty',
    presence_penalty: 'presence_penalty',
    seed: 'seed',
  };

  Object.entries(parameterMappings).forEach(([key, param]) => {
    const enabled = parameterEnabled[key];
    const value = inputs[param];
    const hasValue = value !== undefined && value !== null;

    if (!enabled) {
      return;
    }

    if (param === 'max_tokens') {
      if (typeof value === 'number') {
        payload[param] = value;
      }
      return;
    }

    if (hasValue) {
      payload[param] = value;
    }
  });

  return payload;
};

// 处理API错误响应
export const handleApiError = (error, response = null) => {
  const errorInfo = {
    error: error.message || '未知错误',
    timestamp: new Date().toISOString(),
    stack: error.stack,
  };

  if (response) {
    errorInfo.status = response.status;
    errorInfo.statusText = response.statusText;
  }

  if (error.message.includes('HTTP error')) {
    errorInfo.details = '服务器返回了错误状态码';
  } else if (error.message.includes('Failed to fetch')) {
    errorInfo.details = '网络连接失败或服务器无响应';
  }

  return errorInfo;
};

// 处理模型数据
export const processModelsData = (data, currentModel) => {
  const modelOptions = data.map((model) => ({
    label: model,
    value: model,
  }));

  const hasCurrentModel = modelOptions.some(
    (option) => option.value === currentModel,
  );
  const selectedModel =
    hasCurrentModel && modelOptions.length > 0
      ? currentModel
      : modelOptions[0]?.value;

  return { modelOptions, selectedModel };
};

// 处理分组数据
export const processGroupsData = (data, userGroup) => {
  let groupOptions = Object.entries(data).map(([group, info]) => ({
    label:
      info.desc.length > 20 ? info.desc.substring(0, 20) + '...' : info.desc,
    value: group,
    ratio: info.ratio,
    fullLabel: info.desc,
  }));

  if (groupOptions.length === 0) {
    groupOptions = [
      {
        label: '用户分组',
        value: '',
        ratio: 1,
      },
    ];
  } else if (userGroup) {
    const userGroupIndex = groupOptions.findIndex((g) => g.value === userGroup);
    if (userGroupIndex > -1) {
      const userGroupOption = groupOptions.splice(userGroupIndex, 1)[0];
      groupOptions.unshift(userGroupOption);
    }
  }

  return groupOptions;
};

let channelModels = undefined;
export async function loadChannelModels() {
  const res = await API.get('/api/models');
  const { success, data } = res.data;
  if (!success) {
    return;
  }
  channelModels = data;
  localStorage.setItem('channel_models', JSON.stringify(data));
}

export function getChannelModels(type) {
  if (channelModels !== undefined && type in channelModels) {
    if (!channelModels[type]) {
      return [];
    }
    return channelModels[type];
  }
  let models = localStorage.getItem('channel_models');
  if (!models) {
    return [];
  }
  channelModels = JSON.parse(models);
  if (type in channelModels) {
    return channelModels[type];
  }
  return [];
}
