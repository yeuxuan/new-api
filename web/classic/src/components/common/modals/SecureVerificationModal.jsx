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
import { useTranslation } from 'react-i18next';
import {
  Button,
  Input,
  Modal,
  Select,
  TabPane,
  Tabs,
  Typography,
} from '@douyinfe/semi-ui';

const SecureVerificationModal = ({
  visible,
  verificationMethods,
  verificationState,
  onVerify,
  onCancel,
  onCodeChange,
  onPasswordChange,
  onProviderChange,
  onMethodSwitch,
  title,
  description,
}) => {
  const { t } = useTranslation();
  const {
    has2FA,
    hasPasskey,
    hasPassword,
    hasOAuth,
    oauthProviders = [],
    passkeySupported,
  } = verificationMethods;
  const {
    method,
    loading,
    code = '',
    password = '',
    provider,
  } = verificationState;
  const hasMethod =
    has2FA || (hasPasskey && passkeySupported) || hasPassword || hasOAuth;

  const submit = () => {
    if (method === '2fa') onVerify(method, code);
    else if (method === 'password') onVerify(method, '', password);
    else if (method === 'oauth') onVerify(method, '', '', provider);
    else onVerify(method);
  };

  const handleKeyDown = (event) => {
    if (event.key === 'Enter' && !loading) submit();
    if (event.key === 'Escape' && !loading) onCancel();
  };

  return (
    <Modal
      title={title || t('安全验证')}
      visible={visible}
      onCancel={loading ? undefined : onCancel}
      closeOnEsc={!loading}
      footer={null}
      width={460}
      centered
      style={{ maxWidth: 'calc(100vw - 32px)' }}
    >
      {!hasMethod ? (
        <div className='text-center py-6'>
          <Typography.Title heading={4}>{t('需要安全验证')}</Typography.Title>
          <Typography.Text type='tertiary'>
            {t('当前操作没有可用的安全验证方式')}
          </Typography.Text>
        </div>
      ) : (
        <div>
          {description && (
            <Typography.Paragraph type='tertiary'>
              {description}
            </Typography.Paragraph>
          )}
          <Tabs
            activeKey={method}
            onChange={loading ? undefined : onMethodSwitch}
            type='line'
          >
            {has2FA && (
              <TabPane tab={t('两步验证')} itemKey='2fa'>
                <div className='space-y-4 pt-4'>
                  <Input
                    placeholder={t('请输入6位验证码或8位备用码')}
                    value={code}
                    onChange={onCodeChange}
                    onKeyDown={handleKeyDown}
                    autoFocus={method === '2fa'}
                    disabled={loading}
                    maxLength={8}
                    size='large'
                  />
                  <Typography.Text type='tertiary' size='small'>
                    {t('从认证器应用中获取验证码，或使用备用码')}
                  </Typography.Text>
                </div>
              </TabPane>
            )}
            {hasPasskey && passkeySupported && (
              <TabPane tab={t('Passkey')} itemKey='passkey'>
                <div className='space-y-4 pt-4'>
                  <Typography.Paragraph type='tertiary'>
                    {t('使用已注册的 Passkey 完成验证')}
                  </Typography.Paragraph>
                </div>
              </TabPane>
            )}
            {hasPassword && (
              <TabPane tab={t('密码')} itemKey='password'>
                <div className='space-y-4 pt-4'>
                  <Input
                    mode='password'
                    placeholder={t('请输入当前密码')}
                    value={password}
                    onChange={onPasswordChange}
                    onKeyDown={handleKeyDown}
                    autoFocus={method === 'password'}
                    disabled={loading}
                    size='large'
                  />
                </div>
              </TabPane>
            )}
            {hasOAuth && (
              <TabPane tab={t('账户绑定')} itemKey='oauth'>
                <Select
                  aria-label={t('账户绑定')}
                  value={provider || oauthProviders[0]?.slug}
                  onChange={onProviderChange}
                  disabled={loading}
                  className='w-full mt-4'
                  optionList={oauthProviders.map((item) => ({
                    label: item.name,
                    value: item.slug,
                  }))}
                />
              </TabPane>
            )}
          </Tabs>
          <div className='flex justify-end gap-2 pt-6'>
            <Button onClick={onCancel} disabled={loading}>
              {t('取消')}
            </Button>
            <Button
              theme='solid'
              type='primary'
              loading={loading}
              disabled={
                loading ||
                (method === '2fa' && !code.trim()) ||
                (method === 'password' && !password.trim())
              }
              onClick={submit}
            >
              {t('验证')}
            </Button>
          </div>
        </div>
      )}
    </Modal>
  );
};

export default SecureVerificationModal;
