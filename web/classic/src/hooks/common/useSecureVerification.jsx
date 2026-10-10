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

import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SecureVerificationService } from '../../services/secureVerification';
import { showError, showSuccess } from '../../helpers';

function operationFromOptions(options) {
  return (
    options.operation || {
      scope: options.scope,
      ...(options.context ? { context: options.context } : {}),
    }
  );
}

export const useSecureVerification = ({
  onSuccess,
  onError,
  successMessage,
  autoReset = true,
} = {}) => {
  const { t } = useTranslation();
  const requestRef = useRef(null);
  const submittingRef = useRef(false);
  const [verificationMethods, setVerificationMethods] = useState({
    scope: null,
    methods: [],
    has2FA: false,
    hasPasskey: false,
    hasPassword: false,
    hasSession: false,
    hasOAuth: false,
    oauthProviders: [],
    passkeySupported: false,
    passwordEncryptionEnabled: false,
  });
  const [isModalVisible, setIsModalVisible] = useState(false);
  const [verificationState, setVerificationState] = useState({
    method: null,
    loading: false,
    code: '',
    password: '',
    apiCall: null,
    operation: null,
  });

  const checkVerificationMethods = useCallback(async (scope, signal) => {
    const methods =
      await SecureVerificationService.checkAvailableVerificationMethods(
        scope,
        signal,
      );
    signal?.throwIfAborted();
    setVerificationMethods(methods);
    return methods;
  }, []);

  useEffect(() => () => requestRef.current?.abort(), []);

  const resetState = useCallback(() => {
    requestRef.current?.abort();
    requestRef.current = null;
    setVerificationState({
      method: null,
      loading: false,
      code: '',
      password: '',
      apiCall: null,
      operation: null,
    });
    setIsModalVisible(false);
  }, []);

  const finishVerifiedCall = useCallback(
    async (apiCall, proof, method, signal) => {
      signal.throwIfAborted();
      const result = await apiCall(proof.proof_token, signal);
      signal.throwIfAborted();
      if (result?.success === false) {
        throw new Error(result.message || t('操作失败'));
      }
      if (successMessage) showSuccess(successMessage);
      onSuccess?.(result, method);
      return result;
    },
    [onSuccess, successMessage, t],
  );

  const startVerification = useCallback(
    async (apiCall, options = {}) => {
      if (submittingRef.current) return false;
      requestRef.current?.abort();
      const controller = new AbortController();
      requestRef.current = controller;
      const signal = controller.signal;
      const operation = operationFromOptions(options);
      if (!operation?.scope) {
        const error = new Error(t('验证配置错误'));
        showError(error.message);
        onError?.(error);
        return false;
      }

      let methods;
      try {
        methods = await checkVerificationMethods(operation.scope, signal);
        signal.throwIfAborted();
      } catch (error) {
        if (!signal.aborted) {
          showError(error.message || t('验证失败，请重试'));
          onError?.(error);
        }
        return false;
      }
      const available = new Set(
        methods.methods
          .filter((method) => method.available)
          .map((method) => method.method),
      );
      let defaultMethod = options.preferredMethod;
      if (defaultMethod && !available.has(defaultMethod)) {
        defaultMethod = null;
      }
      if (!defaultMethod) {
        if (available.has('session')) defaultMethod = 'session';
        else if (available.has('passkey')) defaultMethod = 'passkey';
        else if (available.has('2fa')) defaultMethod = '2fa';
        else if (available.has('password')) defaultMethod = 'password';
        else if (available.has('oauth')) defaultMethod = 'oauth';
      }

      if (!defaultMethod) {
        const error = new Error(t('当前操作没有可用的安全验证方式'));
        showError(error.message);
        onError?.(error);
        return false;
      }

      if (defaultMethod === 'session') {
        try {
          const proof = await SecureVerificationService.verify(
            defaultMethod,
            operation,
            '',
            '',
            { signal },
          );
          const result = await finishVerifiedCall(
            apiCall,
            proof,
            defaultMethod,
            signal,
          );
          if (autoReset) resetState();
          return result;
        } catch (error) {
          showError(error.message || t('验证失败，请重试'));
          onError?.(error);
          return false;
        }
      }

      setVerificationState({
        method: defaultMethod,
        loading: false,
        code: '',
        password: options.initialPassword || '',
        apiCall,
        operation,
        provider: methods.oauthProviders[0]?.slug || '',
        title: options.title,
        description: options.description,
      });
      setIsModalVisible(true);
      return true;
    },
    [
      autoReset,
      checkVerificationMethods,
      finishVerifiedCall,
      onError,
      resetState,
      t,
    ],
  );

  const executeVerification = useCallback(
    async (method, code = '', password = '', provider = '') => {
      if (!verificationState.apiCall || !verificationState.operation) {
        showError(t('验证配置错误'));
        return;
      }
      if (submittingRef.current) return;
      const signal = requestRef.current?.signal;
      if (!signal || signal.aborted) return;
      submittingRef.current = true;
      setVerificationState((prev) => ({ ...prev, loading: true }));
      try {
        const proof = await SecureVerificationService.verify(
          method,
          verificationState.operation,
          code,
          password || verificationState.password,
          {
            signal,
            passwordEncryptionEnabled:
              verificationMethods.passwordEncryptionEnabled,
            provider: provider || verificationState.provider,
          },
        );
        const result = await finishVerifiedCall(
          verificationState.apiCall,
          proof,
          method,
          signal,
        );
        if (autoReset) resetState();
        return result;
      } catch (error) {
        if (!signal.aborted) {
          showError(error.message || t('验证失败，请重试'));
          onError?.(error);
        }
      } finally {
        submittingRef.current = false;
        if (requestRef.current?.signal === signal) {
          setVerificationState((prev) => ({
            ...prev,
            loading: false,
            code: '',
            password: '',
          }));
        }
      }
    },
    [
      autoReset,
      finishVerifiedCall,
      onError,
      resetState,
      t,
      verificationState,
      verificationMethods.passwordEncryptionEnabled,
    ],
  );

  const setVerificationCode = useCallback((code) => {
    setVerificationState((prev) => ({ ...prev, code }));
  }, []);

  const setVerificationPassword = useCallback((password) => {
    setVerificationState((prev) => ({ ...prev, password }));
  }, []);

  const setVerificationProvider = useCallback((provider) => {
    setVerificationState((prev) => ({ ...prev, provider }));
  }, []);

  const switchVerificationMethod = useCallback((method) => {
    if (!submittingRef.current) {
      setVerificationState((prev) => ({
        ...prev,
        method,
        code: '',
        password: '',
      }));
    }
  }, []);

  const cancelVerification = useCallback(() => resetState(), [resetState]);

  const canUseMethod = useCallback(
    (method) => {
      if (method === 'session') return verificationMethods.hasSession;
      if (method === 'password') return verificationMethods.hasPassword;
      if (method === 'oauth') return verificationMethods.hasOAuth;
      if (method === '2fa') return verificationMethods.has2FA;
      if (method === 'passkey') {
        return (
          verificationMethods.hasPasskey && verificationMethods.passkeySupported
        );
      }
      return false;
    },
    [verificationMethods],
  );

  const withVerification = useCallback(
    async (apiCall, options = {}) => startVerification(apiCall, options),
    [startVerification],
  );

  return {
    isModalVisible,
    verificationMethods,
    verificationState,
    startVerification,
    executeVerification,
    cancelVerification,
    resetState,
    setVerificationCode,
    setVerificationPassword,
    setVerificationProvider,
    switchVerificationMethod,
    checkVerificationMethods,
    canUseMethod,
    getRecommendedMethod: () =>
      ['session', 'passkey', '2fa', 'password', 'oauth'].find(canUseMethod) ||
      null,
    withVerification,
    hasAnyVerificationMethod:
      verificationMethods.has2FA ||
      verificationMethods.hasPasskey ||
      verificationMethods.hasPassword ||
      verificationMethods.hasOAuth ||
      verificationMethods.hasSession,
    isLoading: verificationState.loading,
    currentMethod: verificationState.method,
    code: verificationState.code,
  };
};
