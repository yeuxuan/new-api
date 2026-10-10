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
import { useNavigate } from 'react-router-dom';
import {
  API,
  copy,
  showError,
  showInfo,
  showSuccess,
  setStatusData,
  prepareCredentialCreationOptions,
  buildRegistrationResult,
  isPasskeySupported,
  setUserData,
  renderQuota,
  getStoredUser,
  clearAuthSession,
} from '../../helpers';
import { UserContext } from '../../context/User';
import { useTranslation } from 'react-i18next';

// 导入子组件
import UserInfoHeader from './personal/components/UserInfoHeader';
import AccountManagement from './personal/cards/AccountManagement';
import NotificationSettings from './personal/cards/NotificationSettings';
import PreferencesSettings from './personal/cards/PreferencesSettings';
import CheckinCalendar from './personal/cards/CheckinCalendar';
import BonusQuotaGrants from './personal/cards/BonusQuotaGrants';
import EmailBindModal from './personal/modals/EmailBindModal';
import WeChatBindModal from './personal/modals/WeChatBindModal';
import AccountDeleteModal from './personal/modals/AccountDeleteModal';
import ChangePasswordModal from './personal/modals/ChangePasswordModal';
import SecureVerificationModal from '../common/modals/SecureVerificationModal';
import { useSecureVerification } from '../../hooks/common/useSecureVerification';
import { logoutAuthentication } from '../../helpers/api';

const PersonalSetting = () => {
  const [userState, userDispatch] = useContext(UserContext);
  let navigate = useNavigate();
  const { t } = useTranslation();

  const [inputs, setInputs] = useState({
    wechat_verification_code: '',
    email_verification_code: '',
    old_email_verification_code: '',
    email: '',
    self_account_deletion_confirmation: '',
    original_password: '',
    set_new_password: '',
    set_new_password_confirmation: '',
  });
  const [status, setStatus] = useState({});
  const [showChangePasswordModal, setShowChangePasswordModal] = useState(false);
  const [showWeChatBindModal, setShowWeChatBindModal] = useState(false);
  const [showEmailBindModal, setShowEmailBindModal] = useState(false);
  const [showAccountDeleteModal, setShowAccountDeleteModal] = useState(false);
  const [turnstileEnabled, setTurnstileEnabled] = useState(false);
  const [turnstileSiteKey, setTurnstileSiteKey] = useState('');
  const [loading, setLoading] = useState(false);
  const [emailBindingFlow, setEmailBindingFlow] = useState(null);
  const [disableButton, setDisableButton] = useState(false);
  const [countdown, setCountdown] = useState(30);
  const [systemToken, setSystemToken] = useState('');
  const [passkeyStatus, setPasskeyStatus] = useState({ enabled: false });
  const [passkeyRegisterLoading, setPasskeyRegisterLoading] = useState(false);
  const [passkeyDeleteLoading, setPasskeyDeleteLoading] = useState(false);
  const [passkeySupported, setPasskeySupported] = useState(false);
  const [securityPending, setSecurityPending] = useState(false);
  const securityOperation = useRef(null);
  const emailRequest = useRef(null);
  const hasPassword = userState?.user?.has_password !== false;
  const [notificationSettings, setNotificationSettings] = useState({
    warningType: 'email',
    warningThreshold: 100000,
    webhookUrl: '',
    webhookSecret: '',
    notificationEmail: '',
    barkUrl: '',
    gotifyUrl: '',
    gotifyToken: '',
    gotifyPriority: 5,
    upstreamModelUpdateNotifyEnabled: false,
    acceptUnsetModelRatioModel: false,
    recordIpLog: false,
  });

  const {
    isModalVisible: isVerificationModalVisible,
    verificationMethods,
    verificationState,
    startVerification,
    executeVerification,
    cancelVerification,
    setVerificationCode,
    setVerificationPassword,
    setVerificationProvider,
    switchVerificationMethod,
  } = useSecureVerification({
    onSuccess: () => {
      securityOperation.current = null;
      setSecurityPending(false);
    },
    onError: () => cancelPersonalVerification(),
  });

  const cancelPersonalVerification = () => {
    securityOperation.current?.abort();
    securityOperation.current = null;
    setSecurityPending(false);
    cancelVerification();
  };

  const startPersonalVerification = async (apiCall, options) => {
    if (securityOperation.current) return;
    const controller = new AbortController();
    securityOperation.current = controller;
    setSecurityPending(true);
    try {
      const result = await startVerification(async (proofToken, signal) => {
        controller.signal.throwIfAborted();
        if (!proofToken) throw new Error(t('验证失败，请重试'));
        return apiCall(proofToken, signal || controller.signal);
      }, options);
      if (result === false) cancelPersonalVerification();
    } catch (error) {
      cancelPersonalVerification();
      showError(error.message || t('操作失败，请重试'));
    }
  };

  useEffect(() => {
    setSystemToken('');
    setEmailBindingFlow(null);
    setShowEmailBindModal(false);
    setShowWeChatBindModal(false);
    setShowChangePasswordModal(false);
    setShowAccountDeleteModal(false);
    setInputs((previous) =>
      Object.fromEntries(Object.keys(previous).map((key) => [key, ''])),
    );
    return () => {
      emailRequest.current?.abort();
      emailRequest.current = null;
      securityOperation.current?.abort();
      securityOperation.current = null;
      cancelVerification();
      setSecurityPending(false);
    };
  }, [userState?.user?.id, userState?.user?.session?.sid, cancelVerification]);

  useEffect(() => {
    let saved = localStorage.getItem('status');
    if (saved) {
      const parsed = JSON.parse(saved);
      setStatus(parsed);
      if (parsed.turnstile_check) {
        setTurnstileEnabled(true);
        setTurnstileSiteKey(parsed.turnstile_site_key);
      } else {
        setTurnstileEnabled(false);
        setTurnstileSiteKey('');
      }
    }
    // Always refresh status from server to avoid stale flags (e.g., admin just enabled OAuth)
    (async () => {
      try {
        const res = await API.get('/api/status');
        const { success, data } = res.data;
        if (success && data) {
          setStatus(data);
          setStatusData(data);
          if (data.turnstile_check) {
            setTurnstileEnabled(true);
            setTurnstileSiteKey(data.turnstile_site_key);
          } else {
            setTurnstileEnabled(false);
            setTurnstileSiteKey('');
          }
        }
      } catch (e) {
        // ignore and keep local status
      }
    })();

    getUserData();

    isPasskeySupported()
      .then(setPasskeySupported)
      .catch(() => setPasskeySupported(false));
  }, []);

  useEffect(() => {
    let countdownInterval = null;
    if (disableButton && countdown > 0) {
      countdownInterval = setInterval(() => {
        setCountdown(countdown - 1);
      }, 1000);
    } else if (countdown === 0) {
      setDisableButton(false);
      setCountdown(0);
    }
    return () => clearInterval(countdownInterval); // Clean up on unmount
  }, [disableButton, countdown]);

  useEffect(() => {
    if (!emailBindingFlow) return;
    const timer = setTimeout(
      () => {
        setEmailBindingFlow(null);
        setDisableButton(false);
        setInputs((previous) => ({
          ...previous,
          email_verification_code: '',
          old_email_verification_code: '',
        }));
      },
      Math.max(0, emailBindingFlow.expires_at * 1000 - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [emailBindingFlow]);

  useEffect(() => {
    if (userState?.user?.setting) {
      const settings = JSON.parse(userState.user.setting);
      setNotificationSettings({
        warningType: settings.notify_type || 'email',
        warningThreshold: settings.quota_warning_threshold || 500000,
        webhookUrl: settings.webhook_url || '',
        webhookSecret: settings.webhook_secret || '',
        notificationEmail: settings.notification_email || '',
        barkUrl: settings.bark_url || '',
        gotifyUrl: settings.gotify_url || '',
        gotifyToken: settings.gotify_token || '',
        gotifyPriority:
          settings.gotify_priority !== undefined ? settings.gotify_priority : 5,
        upstreamModelUpdateNotifyEnabled:
          settings.upstream_model_update_notify_enabled === true,
        acceptUnsetModelRatioModel:
          settings.accept_unset_model_ratio_model || false,
        recordIpLog: settings.record_ip_log || false,
      });
    }
  }, [userState?.user?.setting]);

  const handleInputChange = (name, value) => {
    if (name === 'email') {
      setEmailBindingFlow(null);
      setDisableButton(false);
    }
    setInputs((inputs) => ({ ...inputs, [name]: value }));
  };

  const closeEmailBindModal = (visible) => {
    if (!visible) {
      cancelPersonalVerification();
      emailRequest.current?.abort();
      emailRequest.current = null;
      setLoading(false);
      setEmailBindingFlow(null);
      setDisableButton(false);
      setInputs((previous) => ({
        ...previous,
        email: '',
        email_verification_code: '',
        old_email_verification_code: '',
      }));
    }
    setShowEmailBindModal(visible);
  };

  const closePasswordModal = (visible) => {
    if (!visible) {
      cancelPersonalVerification();
      setInputs((previous) => ({
        ...previous,
        original_password: '',
        set_new_password: '',
        set_new_password_confirmation: '',
      }));
    }
    setShowChangePasswordModal(visible);
  };

  const generateAccessToken = async () => {
    await startPersonalVerification(
      async (proofToken, signal) => {
        const res = await API.post('/api/user/token', undefined, {
          headers: { 'X-Security-Proof': proofToken },
          singleUseAuthorization: true,
          skipErrorHandler: true,
          signal,
        });
        const { success, message, data } = res.data;
        if (!success || !data)
          throw new Error(message || t('操作失败，请重试'));
        signal.throwIfAborted();
        setSystemToken(data);
        if (await copy(data).catch(() => false)) {
          showSuccess(t('令牌已重置并已复制到剪贴板'));
        } else {
          showInfo(t('无法复制到剪贴板，请手动复制'));
        }
        return res.data;
      },
      { scope: 'access_token.generate' },
    );
  };

  const loadPasskeyStatus = async () => {
    try {
      const res = await API.get('/api/user/passkey');
      const { success, data, message } = res.data;
      if (success) {
        setPasskeyStatus({
          enabled: data?.enabled || false,
          last_used_at: data?.last_used_at || null,
          backup_eligible: data?.backup_eligible || false,
          backup_state: data?.backup_state || false,
        });
      } else {
        showError(message);
      }
    } catch (error) {
      // 忽略错误，保留默认状态
    }
  };

  const registerPasskey = async (proofToken, signal) => {
    setPasskeyRegisterLoading(true);
    try {
      const beginRes = await API.post(
        '/api/user/passkey/register/begin',
        undefined,
        {
          headers: { 'X-Security-Proof': proofToken },
          singleUseAuthorization: true,
          skipErrorHandler: true,
          signal,
        },
      );
      const { success, message, data } = beginRes.data;
      if (
        !success ||
        !data?.flow_token ||
        data.expires_at * 1000 <= Date.now()
      ) {
        throw new Error(message || t('无法发起 Passkey 注册'));
      }

      const publicKey = prepareCredentialCreationOptions(
        data?.options || data?.publicKey || data,
      );
      signal.throwIfAborted();
      const credential = await navigator.credentials.create({
        publicKey,
        signal,
      });
      const payload = buildRegistrationResult(credential);
      if (!payload) {
        throw new Error(t('Passkey 注册失败，请重试'));
      }

      signal.throwIfAborted();
      const finishRes = await API.post(
        '/api/user/passkey/register/finish',
        { flow_token: data.flow_token, credential: payload },
        {
          singleUseAuthorization: true,
          acceptAuthRotation: true,
          skipErrorHandler: true,
          signal,
        },
      );
      if (!finishRes.data.success) {
        throw new Error(
          finishRes.data.message || t('Passkey 注册失败，请重试'),
        );
      }

      signal.throwIfAborted();
      showSuccess(t('Passkey 注册成功'));
      await loadPasskeyStatus();
      return finishRes.data;
    } catch (error) {
      if (error?.name === 'AbortError' || error?.name === 'NotAllowedError') {
        showInfo(t('已取消 Passkey 注册'));
        return { cancelled: true };
      }
      throw new Error(error?.message || t('Passkey 注册失败，请重试'));
    } finally {
      setPasskeyRegisterLoading(false);
    }
  };

  const handleRegisterPasskey = async () => {
    if (!passkeySupported || !window.PublicKeyCredential) {
      showInfo(t('当前设备不支持 Passkey'));
      return;
    }
    await startPersonalVerification(registerPasskey, {
      scope: 'passkey.register',
    });
  };

  const removePasskey = async (proofToken, signal) => {
    setPasskeyDeleteLoading(true);
    try {
      const res = await API.delete('/api/user/passkey', {
        headers: { 'X-Security-Proof': proofToken },
        singleUseAuthorization: true,
        acceptAuthRotation: true,
        skipErrorHandler: true,
        signal,
      });
      const { success, message } = res.data;
      if (!success) {
        throw new Error(message || t('操作失败，请重试'));
      }

      signal.throwIfAborted();
      showSuccess(t('Passkey 已解绑'));
      await loadPasskeyStatus();
      return res.data;
    } catch (error) {
      throw new Error(error?.message || t('操作失败，请重试'));
    } finally {
      setPasskeyDeleteLoading(false);
    }
  };

  const handleRemovePasskey = async () => {
    await startPersonalVerification(removePasskey, { scope: 'passkey.delete' });
  };

  const getUserData = async () => {
    try {
      const res = await API.get('/api/user/self', { skipErrorHandler: true });
      const { success, message, data } = res.data;
      if (!success) throw new Error(message || t('操作失败，请重试'));
      const stored = getStoredUser();
      const user = {
        ...data,
        ...(stored?.id === data.id && stored.session
          ? { session: stored.session }
          : {}),
      };
      setUserData(user);
      userDispatch({ type: 'login', payload: user });
      await loadPasskeyStatus();
    } catch (error) {
      showError(error.message || t('操作失败，请重试'));
    }
  };

  const handleSystemTokenClick = async (e) => {
    e.target.select();
    await copy(e.target.value);
    showSuccess(t('系统令牌已复制到剪切板'));
  };

  const deleteAccount = async () => {
    if (inputs.self_account_deletion_confirmation !== userState.user.username) {
      showError(t('请输入你的账户名以确认删除！'));
      return;
    }
    const deletingSessionId = userState.user?.session?.sid;

    await startPersonalVerification(
      async (proofToken, signal) => {
        if (
          !deletingSessionId ||
          getStoredUser()?.session?.sid !== deletingSessionId
        ) {
          throw new Error(t('未登录或登录已过期，请重新登录'));
        }
        const res = await API.delete('/api/user/self', {
          headers: { 'X-Security-Proof': proofToken },
          singleUseAuthorization: true,
          skipErrorHandler: true,
          signal,
        });
        if (!res.data.success)
          throw new Error(res.data.message || t('操作失败，请重试'));
        showSuccess(t('账户已删除！'));
        // Deletion already revokes sessions; cookie cleanup must not block logout.
        try {
          if (getStoredUser()?.session?.sid === deletingSessionId) {
            await logoutAuthentication();
          }
        } catch {
          // The deleted session may already be rejected by the server.
        } finally {
          const currentSessionId = getStoredUser()?.session?.sid;
          if (!currentSessionId || currentSessionId === deletingSessionId) {
            clearAuthSession();
            userDispatch({ type: 'logout' });
            setSystemToken('');
            navigate('/login', { replace: true });
          }
        }
        return res.data;
      },
      { scope: 'account.delete' },
    );
  };

  const bindWeChat = async () => {
    const code = inputs.wechat_verification_code.trim();
    if (!code) return;
    await startPersonalVerification(
      async (proofToken, signal) => {
        const res = await API.post(
          '/api/oauth/wechat/bind',
          { code },
          {
            headers: { 'X-Security-Proof': proofToken },
            singleUseAuthorization: true,
            skipErrorHandler: true,
            signal,
          },
        );
        if (!res.data.success)
          throw new Error(res.data.message || t('操作失败，请重试'));
        signal.throwIfAborted();
        showSuccess(t('微信账户绑定成功！'));
        setShowWeChatBindModal(false);
        handleInputChange('wechat_verification_code', '');
        await getUserData();
        return res.data;
      },
      { scope: 'account.binding.bind', context: { provider: 'wechat', code } },
    );
  };

  const changePassword = async () => {
    if (hasPassword && inputs.original_password === '') {
      showError(t('请输入原密码'));
      return;
    }
    if (inputs.set_new_password === '') {
      showError(t('请输入新密码！'));
      return;
    }
    if ([...inputs.set_new_password].length < 8) {
      showError(t('密码长度至少为8个字符'));
      return;
    }
    if (hasPassword && inputs.original_password === inputs.set_new_password) {
      showError(t('新密码需要和原密码不一致！'));
      return;
    }
    if (inputs.set_new_password !== inputs.set_new_password_confirmation) {
      showError(t('两次输入的密码不一致！'));
      return;
    }
    const payload = {
      password: inputs.set_new_password,
      ...(hasPassword ? { original_password: inputs.original_password } : {}),
    };
    await startPersonalVerification(
      async (proofToken, signal) => {
        const res = await API.put('/api/user/self', payload, {
          headers: { 'X-Security-Proof': proofToken },
          singleUseAuthorization: true,
          acceptAuthRotation: true,
          skipErrorHandler: true,
          signal,
        });
        if (!res.data.success)
          throw new Error(res.data.message || t('操作失败，请重试'));
        signal.throwIfAborted();
        showSuccess(t('密码修改成功！'));
        setShowChangePasswordModal(false);
        setInputs((previous) => ({
          ...previous,
          original_password: '',
          set_new_password: '',
          set_new_password_confirmation: '',
        }));
        await getUserData();
        return res.data;
      },
      {
        scope: hasPassword ? 'account.password.change' : 'account.password.set',
        initialPassword: hasPassword ? inputs.original_password : undefined,
      },
    );
  };

  const applyEmailBindingFlow = (flow) => {
    if (
      !flow?.flow_token ||
      !Number.isFinite(flow.expires_at) ||
      flow.expires_at * 1000 <= Date.now()
    ) {
      throw new Error(t('操作失败，请重试'));
    }
    setEmailBindingFlow(flow);
    setInputs((previous) => ({
      ...previous,
      email: flow.email,
      email_verification_code: '',
      old_email_verification_code: '',
    }));
    const seconds = Math.max(0, Math.ceil(flow.resend_at - Date.now() / 1000));
    setCountdown(seconds);
    setDisableButton(seconds > 0);
    showSuccess(t('验证码发送成功，请检查邮箱！'));
  };

  const sendVerificationCode = async () => {
    if (emailRequest.current || securityOperation.current || disableButton)
      return;
    const email = inputs.email.trim().toLowerCase();
    if (email === '') {
      showError(t('请输入邮箱！'));
      return;
    }
    if (!emailBindingFlow || emailBindingFlow.expires_at * 1000 <= Date.now()) {
      await startPersonalVerification(
        async (proofToken, signal) => {
          const res = await API.post(
            '/api/oauth/email/bind/start',
            { email },
            {
              headers: { 'X-Security-Proof': proofToken },
              singleUseAuthorization: true,
              skipErrorHandler: true,
              signal,
            },
          );
          if (!res.data.success)
            throw new Error(res.data.message || t('操作失败，请重试'));
          signal.throwIfAborted();
          applyEmailBindingFlow(res.data.data);
          return res.data;
        },
        {
          scope: 'account.binding.bind',
          context: { provider: 'email', email },
        },
      );
      return;
    }
    const controller = new AbortController();
    emailRequest.current = controller;
    setLoading(true);
    try {
      const res = await API.post(
        '/api/oauth/email/bind/resend',
        { flow_token: emailBindingFlow.flow_token },
        {
          singleUseAuthorization: true,
          skipErrorHandler: true,
          signal: controller.signal,
        },
      );
      controller.signal.throwIfAborted();
      if (!res.data.success)
        throw Object.assign(
          new Error(res.data.message || t('操作失败，请重试')),
          { code: res.data.code },
        );
      applyEmailBindingFlow(res.data.data);
    } catch (error) {
      if (!controller.signal.aborted) handleEmailBindingError(error);
    } finally {
      if (emailRequest.current === controller) {
        emailRequest.current = null;
        setLoading(false);
      }
    }
  };

  const handleEmailBindingError = (error) => {
    const code = error.response?.data?.code || error.code;
    if (
      [
        'AUTH_FLOW_INVALID',
        'EMAIL_BINDING_LOCKED',
        'ACCOUNT_SECURITY_STATE_CHANGED',
        'EMAIL_BINDING_DELIVERY_FAILED',
      ].includes(code)
    ) {
      setEmailBindingFlow(null);
      setDisableButton(false);
    }
    showError(
      error.response?.data?.message || error.message || t('操作失败，请重试'),
    );
  };

  const bindEmail = async () => {
    if (emailRequest.current || securityOperation.current) return;
    if (!emailBindingFlow || emailBindingFlow.expires_at * 1000 <= Date.now()) {
      setEmailBindingFlow(null);
      setDisableButton(false);
      await sendVerificationCode();
      return;
    }
    if (
      !inputs.email_verification_code.trim() ||
      (emailBindingFlow.old_email_required &&
        !inputs.old_email_verification_code.trim())
    ) {
      showError(t('请输入邮箱验证码！'));
      return;
    }
    const controller = new AbortController();
    emailRequest.current = controller;
    setLoading(true);
    try {
      const res = await API.post(
        '/api/oauth/email/bind',
        {
          flow_token: emailBindingFlow.flow_token,
          new_code: inputs.email_verification_code.trim(),
          old_code: emailBindingFlow.old_email_required
            ? inputs.old_email_verification_code.trim()
            : '',
        },
        {
          singleUseAuthorization: true,
          skipErrorHandler: true,
          signal: controller.signal,
        },
      );
      controller.signal.throwIfAborted();
      if (!res.data.success)
        throw Object.assign(
          new Error(res.data.message || t('操作失败，请重试')),
          { code: res.data.code },
        );
      const { quota_awarded, expires_at } = res.data.data || res.data;
      if ((quota_awarded ?? 0) > 0) {
        const amount = renderQuota(quota_awarded);
        if ((expires_at ?? 0) > 0) {
          const date = new Date(expires_at * 1000).toLocaleString();
          showSuccess(
            t(
              'Email bound successfully! You received {{amount}} bonus quota, valid until {{date}}.',
              { amount, date },
            ),
          );
        } else {
          showSuccess(
            t(
              'Email bound successfully! You received {{amount}} bonus quota (never expires).',
              { amount },
            ),
          );
        }
      } else {
        showSuccess(t('邮箱账户绑定成功！'));
      }
      setShowEmailBindModal(false);
      setEmailBindingFlow(null);
      setInputs((previous) => ({
        ...previous,
        email: '',
        email_verification_code: '',
        old_email_verification_code: '',
      }));
      await getUserData();
    } catch (error) {
      if (!controller.signal.aborted) handleEmailBindingError(error);
    } finally {
      if (emailRequest.current === controller) {
        emailRequest.current = null;
        setLoading(false);
      }
    }
  };

  const handleNotificationSettingChange = (type, value) => {
    setNotificationSettings((prev) => ({
      ...prev,
      [type]: value.target
        ? value.target.value !== undefined
          ? value.target.value
          : value.target.checked
        : value, // handle checkbox properly
    }));
  };

  const saveNotificationSettings = async () => {
    try {
      const res = await API.put('/api/user/setting', {
        notify_type: notificationSettings.warningType,
        quota_warning_threshold: parseFloat(
          notificationSettings.warningThreshold,
        ),
        webhook_url: notificationSettings.webhookUrl,
        webhook_secret: notificationSettings.webhookSecret,
        notification_email: notificationSettings.notificationEmail,
        bark_url: notificationSettings.barkUrl,
        gotify_url: notificationSettings.gotifyUrl,
        gotify_token: notificationSettings.gotifyToken,
        gotify_priority: (() => {
          const parsed = parseInt(notificationSettings.gotifyPriority);
          return isNaN(parsed) ? 5 : parsed;
        })(),
        upstream_model_update_notify_enabled:
          notificationSettings.upstreamModelUpdateNotifyEnabled === true,
        accept_unset_model_ratio_model:
          notificationSettings.acceptUnsetModelRatioModel,
        record_ip_log: notificationSettings.recordIpLog,
      });

      if (res.data.success) {
        showSuccess(t('设置保存成功'));
        await getUserData();
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError(t('设置保存失败'));
    }
  };

  return (
    <div className='mt-[60px]'>
      <div className='flex justify-center'>
        <div className='w-full max-w-7xl mx-auto px-2'>
          {/* 顶部用户信息区域 */}
          <UserInfoHeader t={t} userState={userState} status={status} />

          <BonusQuotaGrants t={t} userState={userState} status={status} />

          {/* 签到日历 - 仅在启用时显示 */}
          {status?.checkin_enabled && (
            <div className='mt-4 md:mt-6'>
              <CheckinCalendar
                t={t}
                status={status}
                turnstileEnabled={turnstileEnabled}
                turnstileSiteKey={turnstileSiteKey}
                onUserRefresh={getUserData}
              />
            </div>
          )}

          {/* 账户管理和其他设置 */}
          <div className='grid grid-cols-1 xl:grid-cols-2 items-start gap-4 md:gap-6 mt-4 md:mt-6'>
            {/* 左侧：账户管理设置 */}
            <div className='flex flex-col gap-4 md:gap-6'>
              <AccountManagement
                t={t}
                userState={userState}
                status={status}
                systemToken={systemToken}
                setShowEmailBindModal={setShowEmailBindModal}
                setShowWeChatBindModal={setShowWeChatBindModal}
                generateAccessToken={generateAccessToken}
                handleSystemTokenClick={handleSystemTokenClick}
                setShowChangePasswordModal={setShowChangePasswordModal}
                setShowAccountDeleteModal={setShowAccountDeleteModal}
                passkeyStatus={passkeyStatus}
                passkeySupported={passkeySupported}
                passkeyRegisterLoading={passkeyRegisterLoading}
                passkeyDeleteLoading={passkeyDeleteLoading}
                onPasskeyRegister={handleRegisterPasskey}
                onPasskeyDelete={handleRemovePasskey}
                onUpdate={getUserData}
              />

              {/* 偏好设置（语言等） */}
              <PreferencesSettings t={t} />
            </div>

            {/* 右侧：其他设置 */}
            <NotificationSettings
              t={t}
              notificationSettings={notificationSettings}
              handleNotificationSettingChange={handleNotificationSettingChange}
              saveNotificationSettings={saveNotificationSettings}
            />
          </div>
        </div>
      </div>

      {/* 模态框组件 */}
      <EmailBindModal
        t={t}
        showEmailBindModal={showEmailBindModal && !isVerificationModalVisible}
        setShowEmailBindModal={closeEmailBindModal}
        inputs={inputs}
        handleInputChange={handleInputChange}
        sendVerificationCode={sendVerificationCode}
        bindEmail={bindEmail}
        disableButton={disableButton}
        loading={loading || securityPending}
        countdown={countdown}
        flow={emailBindingFlow}
      />

      <WeChatBindModal
        t={t}
        showWeChatBindModal={showWeChatBindModal && !isVerificationModalVisible}
        setShowWeChatBindModal={(visible) => {
          if (!visible) {
            cancelPersonalVerification();
            handleInputChange('wechat_verification_code', '');
          }
          setShowWeChatBindModal(visible);
        }}
        inputs={inputs}
        handleInputChange={handleInputChange}
        bindWeChat={bindWeChat}
        status={status}
      />

      <AccountDeleteModal
        t={t}
        showAccountDeleteModal={
          showAccountDeleteModal && !isVerificationModalVisible
        }
        setShowAccountDeleteModal={(visible) => {
          if (!visible) {
            cancelPersonalVerification();
            handleInputChange('self_account_deletion_confirmation', '');
          }
          setShowAccountDeleteModal(visible);
        }}
        inputs={inputs}
        handleInputChange={handleInputChange}
        deleteAccount={deleteAccount}
        userState={userState}
      />

      <ChangePasswordModal
        t={t}
        showChangePasswordModal={
          showChangePasswordModal && !isVerificationModalVisible
        }
        setShowChangePasswordModal={closePasswordModal}
        inputs={inputs}
        handleInputChange={handleInputChange}
        changePassword={changePassword}
        hasPassword={hasPassword}
        loading={securityPending}
      />

      <SecureVerificationModal
        visible={isVerificationModalVisible}
        verificationMethods={verificationMethods}
        verificationState={verificationState}
        onVerify={(...args) => executeVerification(...args).catch(() => {})}
        onCancel={cancelPersonalVerification}
        onCodeChange={setVerificationCode}
        onPasswordChange={setVerificationPassword}
        onProviderChange={setVerificationProvider}
        onMethodSwitch={switchVerificationMethod}
        title={verificationState.title}
        description={verificationState.description}
      />
    </div>
  );
};

export default PersonalSetting;
