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

import React, { useContext, useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { API, clearAuthSession, showError, showSuccess } from '../../helpers';
import { UserContext } from '../../context/User';
import Loading from '../common/ui/Loading';
import TwoFAVerification from './TwoFAVerification';
import { completeLogin, isLoginChallenge } from './loginFlow';
import {
  getOAuthCallbackMode,
  OAUTH_POPUP_CALLBACK_MESSAGE,
  OAUTH_POPUP_RESULT_MESSAGE,
} from '../../helpers/oauthVerification';

const OAuth2Callback = (props) => {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const [, userDispatch] = useContext(UserContext);
  const navigate = useNavigate();
  const [loginChallenge, setLoginChallenge] = useState(null);
  const hasExecuted = useRef(false);

  useEffect(() => {
    // 防止 React 18 Strict Mode 下重复执行
    if (hasExecuted.current) {
      return;
    }
    hasExecuted.current = true;

    const code = searchParams.get('code') || '';
    const state = searchParams.get('state') || '';
    const error = searchParams.get('error') || '';
    const errorDescription = searchParams.get('error_description') || '';
    const mode = getOAuthCallbackMode(props.type, state);

    if (mode === 'bind' || mode === 'verify') {
      const opener = window.opener;
      if (!opener || opener.closed || !state) {
        showError(t('授权窗口已失效，请重试'));
        return;
      }
      let active = true;
      const closeTimer = window.setTimeout(
        () => {
          if (active) {
            showError(t('授权超时，请重试'));
            window.close();
          }
        },
        10 * 60 * 1000,
      );
      const onResult = (event) => {
        if (
          !active ||
          event.origin !== window.location.origin ||
          event.source !== opener
        ) {
          return;
        }
        const result = event.data;
        if (
          !result ||
          result.type !== OAUTH_POPUP_RESULT_MESSAGE ||
          result.intent !== mode ||
          result.provider !== props.type ||
          result.state !== state
        ) {
          return;
        }
        active = false;
        window.clearTimeout(closeTimer);
        window.removeEventListener('message', onResult);
        if (result.success) {
          showSuccess(mode === 'bind' ? t('绑定成功！') : t('验证成功！'));
          window.close();
        } else {
          showError(result.message || t('授权失败'));
          window.setTimeout(() => window.close(), 1500);
        }
      };
      window.addEventListener('message', onResult);
      opener.postMessage(
        {
          type: OAUTH_POPUP_CALLBACK_MESSAGE,
          intent: mode,
          provider: props.type,
          state,
          code: code || undefined,
          error: error || undefined,
          errorDescription: errorDescription || undefined,
        },
        window.location.origin,
      );
      return () => {
        active = false;
        window.clearTimeout(closeTimer);
        window.removeEventListener('message', onResult);
      };
    }

    if (!code && !error) {
      showError(t('未获取到授权码'));
      navigate('/login', { replace: true });
      return;
    }

    clearAuthSession();
    const exchange = async () => {
      try {
        const response = await API.get(
          `/api/oauth/${encodeURIComponent(props.type)}`,
          {
            params: {
              code: code || undefined,
              state,
              error: error || undefined,
              error_description: errorDescription || undefined,
            },
            skipAuthRefresh: true,
            skipErrorHandler: true,
            disableDuplicate: true,
          },
        );
        if (!response.data?.success) {
          throw new Error(response.data?.message || t('授权失败'));
        }
        if (isLoginChallenge(response.data.data)) {
          setLoginChallenge(response.data.data);
          return;
        }
        completeLogin(response.data.data, userDispatch);
        showSuccess(t('登录成功！'));
        navigate('/', { replace: true });
      } catch (requestError) {
        showError(requestError?.message || t('授权失败'));
        navigate('/login', { replace: true });
      }
    };
    void exchange();
  }, [navigate, props.type, searchParams, t, userDispatch]);

  if (loginChallenge) {
    return (
      <TwoFAVerification
        key={loginChallenge.flow_token}
        challenge={loginChallenge}
        onSuccess={(bundle) => {
          completeLogin(bundle, userDispatch);
          showSuccess(t('登录成功！'));
          navigate('/', { replace: true });
        }}
        onBack={() => {
          setLoginChallenge(null);
          navigate('/login', { replace: true });
        }}
      />
    );
  }

  return <Loading />;
};

export default OAuth2Callback;
