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
import { Modal } from '@douyinfe/semi-ui';
import { Coins, ArrowDown, ShieldCheck, ExternalLink } from 'lucide-react';

const CreemConfirmModal = ({
  t,
  visible,
  onOk,
  onCancel,
  confirmLoading,
  product,
}) => {
  const symbol = product?.currency === 'EUR' ? '€' : '$';

  return (
    <Modal
      title={
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <div
            style={{
              width: 30,
              height: 30,
              borderRadius: 8,
              background: 'linear-gradient(135deg, #f59e0b, #d97706)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              flexShrink: 0,
              boxShadow: '0 2px 8px rgba(245,158,11,0.35)',
            }}
          >
            <ExternalLink size={14} color='#fff' />
          </div>
          <span style={{ fontSize: 15, fontWeight: 700 }}>
            {t('支付确认')}
          </span>
        </div>
      }
      visible={visible}
      onOk={onOk}
      onCancel={onCancel}
      maskClosable={false}
      size='small'
      centered
      confirmLoading={confirmLoading}
      okText={t('前往支付')}
      cancelText={t('取消')}
    >
      {product && (
        <div style={{ padding: '4px 0 8px' }}>
          {/* 中心视觉卡片：实付 → 到账 */}
          <div
            style={{
              borderRadius: 16,
              border: '1.5px solid var(--semi-color-border)',
              background: 'var(--semi-color-bg-2)',
              padding: '20px 24px 18px',
              textAlign: 'center',
              marginBottom: 14,
            }}
          >
            {/* 产品名称 */}
            <div
              style={{
                fontSize: 12,
                fontWeight: 600,
                color: 'var(--semi-color-text-2)',
                letterSpacing: 0.5,
                marginBottom: 14,
                padding: '4px 12px',
                background: 'var(--semi-color-fill-0)',
                borderRadius: 20,
                display: 'inline-block',
              }}
            >
              {product.name}
            </div>

            {/* 实付金额 */}
            <div
              style={{
                fontSize: 11,
                fontWeight: 600,
                color: 'var(--semi-color-text-2)',
                textTransform: 'uppercase',
                letterSpacing: 1,
                marginBottom: 4,
              }}
            >
              {t('实付金额')}
            </div>
            <div
              style={{
                fontSize: 34,
                fontWeight: 800,
                color: 'var(--semi-color-text-0)',
                lineHeight: 1.1,
                letterSpacing: -1,
              }}
            >
              {symbol}
              {product.price}
            </div>

            {/* 分隔线 */}
            <div
              style={{
                margin: '14px 0 12px',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: 8,
              }}
            >
              <div
                style={{
                  flex: 1,
                  height: 1,
                  background: 'var(--semi-color-border)',
                  borderRadius: 1,
                }}
              />
              <div
                style={{
                  width: 24,
                  height: 24,
                  borderRadius: '50%',
                  background: 'var(--semi-color-primary-light-default)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  flexShrink: 0,
                }}
              >
                <ArrowDown
                  size={13}
                  style={{ color: 'var(--semi-color-primary)' }}
                />
              </div>
              <div
                style={{
                  flex: 1,
                  height: 1,
                  background: 'var(--semi-color-border)',
                  borderRadius: 1,
                }}
              />
            </div>

            {/* 平台到账 */}
            <div
              style={{
                fontSize: 11,
                fontWeight: 600,
                color: 'var(--semi-color-text-2)',
                textTransform: 'uppercase',
                letterSpacing: 1,
                marginBottom: 4,
              }}
            >
              {t('平台到账')}
            </div>
            <div
              style={{
                fontSize: 38,
                fontWeight: 900,
                color: 'var(--semi-color-primary)',
                lineHeight: 1.1,
                letterSpacing: -1.5,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: 6,
              }}
            >
              <Coins size={22} style={{ flexShrink: 0, opacity: 0.85 }} />
              {product.quota}&nbsp;{symbol}
            </div>
          </div>

          {/* 底部安全提示 */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: 5,
              fontSize: 12,
              color: 'var(--semi-color-text-2)',
            }}
          >
            <ShieldCheck size={13} style={{ flexShrink: 0 }} />
            <span>{t('将跳转至 Creem 安全页面完成支付')}</span>
          </div>
        </div>
      )}
    </Modal>
  );
};

export default CreemConfirmModal;
