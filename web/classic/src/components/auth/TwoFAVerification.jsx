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

import React, { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { showError } from '../../helpers';
import { getAuthState } from '../../helpers/authSession';
import { isPasskeySupported } from '../../helpers/passkey';
import SecureVerificationModal from '../common/modals/SecureVerificationModal';
import { isLoginChallenge, verifyLoginChallenge } from './loginFlow';

const TwoFAVerification = ({ challenge, onSuccess, onBack }) => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [passkeySupported, setPasskeySupported] = useState(false);
  const [code, setCode] = useState('');
  const has2FA = challenge?.methods?.some(
    (item) => item.method === '2fa' && item.available,
  );
  const hasPasskey = challenge?.methods?.some(
    (item) => item.method === 'passkey' && item.available,
  );
  const [method, setMethod] = useState(has2FA ? '2fa' : 'passkey');
  const request = useRef(null);
  const initialSession = useRef(getAuthState()?.session?.sid);

  useEffect(() => {
    let active = true;
    isPasskeySupported()
      .then((supported) => {
        if (active) setPasskeySupported(supported);
      })
      .catch(() => {});
    return () => {
      active = false;
      request.current?.abort();
    };
  }, []);

  const handleVerify = async (selectedMethod, verificationCode = '') => {
    if (request.current) return;
    if (
      !isLoginChallenge(challenge) ||
      challenge.expires_at * 1000 <= Date.now() ||
      getAuthState()?.session?.sid !== initialSession.current
    ) {
      showError(t('验证失败，请重试'));
      onBack?.();
      return;
    }
    const controller = new AbortController();
    request.current = controller;
    setLoading(true);
    try {
      const bundle = await verifyLoginChallenge(
        challenge,
        selectedMethod,
        verificationCode,
        controller.signal,
      );
      controller.signal.throwIfAborted();
      if (getAuthState()?.session?.sid !== initialSession.current) return;
      onSuccess?.(bundle);
    } catch (error) {
      if (!controller.signal.aborted)
        showError(t(error.message || '验证失败，请重试'));
    } finally {
      if (request.current === controller) {
        request.current = null;
        setLoading(false);
      }
    }
  };

  return (
    <SecureVerificationModal
      visible
      title={t('两步验证')}
      verificationMethods={{
        has2FA,
        hasPasskey,
        passkeySupported,
        hasPassword: false,
      }}
      verificationState={{ method, loading, code, password: '' }}
      onVerify={handleVerify}
      onCancel={() => {
        request.current?.abort();
        setCode('');
        onBack?.();
      }}
      onCodeChange={setCode}
      onMethodSwitch={(nextMethod) => {
        if (loading) return;
        setMethod(nextMethod);
        setCode('');
      }}
    />
  );
};

export default TwoFAVerification;
