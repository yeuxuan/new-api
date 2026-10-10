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
import { Button, Input, Modal } from '@douyinfe/semi-ui';
import { IconMail, IconKey } from '@douyinfe/semi-icons';

const EmailBindModal = ({
  t,
  showEmailBindModal,
  setShowEmailBindModal,
  inputs,
  handleInputChange,
  sendVerificationCode,
  bindEmail,
  disableButton,
  loading,
  countdown,
  flow,
}) => {
  return (
    <Modal
      title={
        <div className='flex items-center'>
          <IconMail className='mr-2 text-blue-500' />
          {t('绑定邮箱地址')}
        </div>
      }
      visible={showEmailBindModal}
      onCancel={() => setShowEmailBindModal(false)}
      onOk={bindEmail}
      confirmLoading={loading}
      okButtonProps={{ disabled: loading }}
      size={'small'}
      centered={true}
      maskClosable={false}
      className='modern-modal'
    >
      <div className='space-y-4 py-4'>
        <div className='flex gap-3'>
          <Input
            placeholder={t('输入邮箱地址')}
            aria-label={t('邮箱地址')}
            value={inputs.email}
            disabled={loading}
            onChange={(value) => handleInputChange('email', value)}
            name='email'
            type='email'
            size='large'
            className='!rounded-lg flex-1'
            prefix={<IconMail />}
          />
          <Button
            onClick={sendVerificationCode}
            disabled={disableButton || loading}
            className='!rounded-lg'
            type='primary'
            theme='outline'
            size='large'
          >
            {disableButton
              ? `${t('重新发送')} (${countdown})`
              : t('获取验证码')}
          </Button>
        </div>

        <Input
          placeholder={flow ? `${flow.email} ${t('验证码')}` : t('验证码')}
          aria-label={flow ? `${flow.email} ${t('验证码')}` : t('验证码')}
          disabled={!flow || loading}
          autoComplete='one-time-code'
          name='email_verification_code'
          value={inputs.email_verification_code}
          onChange={(value) =>
            handleInputChange('email_verification_code', value)
          }
          size='large'
          className='!rounded-lg'
          prefix={<IconKey />}
        />

        {flow?.old_email_required && (
          <Input
            placeholder={`${flow.current_email} ${t('验证码')}`}
            aria-label={`${flow.current_email} ${t('验证码')}`}
            name='old_email_verification_code'
            value={inputs.old_email_verification_code}
            onChange={(value) =>
              handleInputChange('old_email_verification_code', value)
            }
            disabled={loading}
            autoComplete='one-time-code'
            size='large'
            className='!rounded-lg'
            prefix={<IconKey />}
          />
        )}
      </div>
    </Modal>
  );
};

export default EmailBindModal;
