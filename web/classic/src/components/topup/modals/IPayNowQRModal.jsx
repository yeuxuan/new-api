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
import { Modal, Button, Spin, Typography } from '@douyinfe/semi-ui';
import { QRCodeSVG } from 'qrcode.react';
import { SiAlipay, SiWechat } from 'react-icons/si';
import { CheckCircle2, Clock, RefreshCw, AlertTriangle } from 'lucide-react';
import { API, showError, showSuccess } from '../../../helpers';

const { Text } = Typography;

const POLL_INTERVAL_MS = 3000;
const POLL_TIMEOUT_MS = 5 * 60 * 1000;

const formatMMSS = (ms) => {
  const total = Math.max(0, Math.floor(ms / 1000));
  const m = String(Math.floor(total / 60)).padStart(2, '0');
  const s = String(total % 60).padStart(2, '0');
  return `${m}:${s}`;
};

const IPayNowQRModal = ({
  t,
  visible,
  tradeNo,
  qrUrl,
  amount,
  onClose,
  onPaid,
}) => {
  const [status, setStatus] = useState('pending'); // pending | success | expired
  const [checking, setChecking] = useState(false);
  const [remainingMs, setRemainingMs] = useState(POLL_TIMEOUT_MS);
  const [paidAmount, setPaidAmount] = useState(null);
  const timerRef = useRef(null);
  const countdownRef = useRef(null);
  const deadlineRef = useRef(0);

  const stopAll = () => {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
    if (countdownRef.current) {
      clearInterval(countdownRef.current);
      countdownRef.current = null;
    }
  };

  const fetchOrder = async (options = {}) => {
    if (!tradeNo) return;
    const silent = options.silent === true;
    if (!silent) setChecking(true);
    try {
      const res = await API.get(`/api/user/ipaynow/order/${tradeNo}`);
      const { success, data, message } = res.data || {};
      if (success && data) {
        if (data.status === 'success') {
          stopAll();
          setStatus('success');
          if (typeof data.money === 'number') setPaidAmount(data.money);
          if (!silent) showSuccess(t('支付成功'));
          onPaid?.(data);
        } else if (data.status === 'expired') {
          stopAll();
          setStatus('expired');
        }
      } else if (!silent) {
        showError(message || t('查询订单失败'));
      }
    } catch (e) {
      if (!silent) showError(t('查询订单失败'));
    } finally {
      if (!silent) setChecking(false);
    }
  };

  useEffect(() => {
    if (!visible || !tradeNo) {
      stopAll();
      return undefined;
    }
    setStatus('pending');
    deadlineRef.current = Date.now() + POLL_TIMEOUT_MS;
    setRemainingMs(POLL_TIMEOUT_MS);

    const tick = async () => {
      const remain = deadlineRef.current - Date.now();
      if (remain <= 0) {
        stopAll();
        setStatus('expired');
        return;
      }
      await fetchOrder({ silent: true });
    };

    tick();
    timerRef.current = setInterval(tick, POLL_INTERVAL_MS);
    countdownRef.current = setInterval(() => {
      setRemainingMs(Math.max(0, deadlineRef.current - Date.now()));
    }, 1000);

    return () => stopAll();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible, tradeNo]);

  const handleClose = () => {
    stopAll();
    onClose?.();
  };

  const isPending = status === 'pending';
  const isSuccess = status === 'success';
  const isExpired = status === 'expired';

  return (
    <Modal
      visible={visible}
      onCancel={handleClose}
      footer={null}
      header={null}
      maskClosable={false}
      centered
      width={380}
      bodyStyle={{ padding: 0 }}
      style={{ borderRadius: 20, overflow: 'hidden' }}
    >
      <style>{`
        @keyframes qrPulse {
          0%, 100% { box-shadow: 0 0 0 0 rgba(7, 193, 96, 0.22); }
          50% { box-shadow: 0 0 0 10px rgba(7, 193, 96, 0); }
        }
        @keyframes checkPop {
          0% { transform: scale(0.4); opacity: 0; }
          60% { transform: scale(1.08); opacity: 1; }
          100% { transform: scale(1); opacity: 1; }
        }
        @keyframes slideUp {
          from { transform: translateY(4px); opacity: 0; }
          to { transform: translateY(0); opacity: 1; }
        }
      `}</style>

      {/* 顶部：状态徽章 */}
      <div
        style={{
          position: 'relative',
          padding: '18px 20px 4px',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <div
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '4px 10px',
            borderRadius: 999,
            fontSize: 12,
            fontWeight: 600,
            background: isSuccess
              ? 'rgba(7, 193, 96, 0.1)'
              : isExpired
                ? 'rgba(244, 67, 54, 0.08)'
                : 'rgba(22, 119, 255, 0.08)',
            color: isSuccess
              ? '#07C160'
              : isExpired
                ? '#d9363e'
                : '#1677FF',
          }}
        >
          {isPending && <Clock size={12} />}
          {isSuccess && <CheckCircle2 size={12} />}
          {isExpired && <AlertTriangle size={12} />}
          {isPending && `${t('剩余')} ${formatMMSS(remainingMs)}`}
          {isSuccess && t('支付成功')}
          {isExpired && t('订单已失效')}
        </div>
      </div>

      {/* 金额 */}
      <div style={{ textAlign: 'center', padding: '8px 24px 16px' }}>
        <div
          style={{
            fontSize: 11,
            fontWeight: 600,
            color: 'var(--semi-color-text-2)',
            letterSpacing: 1,
            textTransform: 'uppercase',
            marginBottom: 4,
          }}
        >
          {isSuccess ? t('已到账金额') : t('待支付金额')}
        </div>
        <div
          style={{
            display: 'inline-flex',
            alignItems: 'baseline',
            gap: 2,
            color: isSuccess
              ? '#07C160'
              : 'var(--semi-color-text-0)',
            fontWeight: 800,
            letterSpacing: -0.5,
            animation: 'slideUp 0.25s ease',
          }}
          key={status}
        >
          <span style={{ fontSize: 18 }}>¥</span>
          <span style={{ fontSize: 34, lineHeight: 1 }}>
            {(() => {
              const val = isSuccess
                ? paidAmount ?? amount
                : amount;
              if (val === undefined || val === null) return '—';
              const num = Number(val);
              return Number.isFinite(num) ? num.toFixed(2) : '—';
            })()}
          </span>
        </div>
      </div>

      {/* QR 码主区 */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'center',
          padding: '4px 24px 18px',
        }}
      >
        <div
          style={{
            position: 'relative',
            width: 240,
            height: 240,
            borderRadius: 18,
            background: '#fff',
            boxShadow: '0 4px 20px rgba(0,0,0,0.08)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            animation: isPending ? 'qrPulse 2.2s ease-in-out infinite' : 'none',
            border: isPending
              ? '1px solid rgba(7, 193, 96, 0.25)'
              : isSuccess
                ? '1px solid rgba(7, 193, 96, 0.4)'
                : '1px solid rgba(217, 54, 62, 0.25)',
          }}
        >
          {isPending && (
            qrUrl ? (
              <QRCodeSVG
                value={qrUrl}
                size={200}
                level='H'
                includeMargin={false}
              />
            ) : (
              <Spin />
            )
          )}
          {isSuccess && (
            <div
              style={{
                width: 120,
                height: 120,
                borderRadius: '50%',
                background:
                  'linear-gradient(135deg, rgba(7,193,96,0.12), rgba(7,193,96,0.22))',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                animation: 'checkPop 0.45s cubic-bezier(0.34, 1.56, 0.64, 1)',
              }}
            >
              <CheckCircle2 size={72} color='#07C160' strokeWidth={2.2} />
            </div>
          )}
          {isExpired && (
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                gap: 10,
                color: 'var(--semi-color-text-2)',
              }}
            >
              <AlertTriangle size={56} color='#d9363e' strokeWidth={2} />
              <Text type='tertiary' size='small'>
                {t('二维码已过期')}
              </Text>
            </div>
          )}
        </div>
      </div>

      {/* 扫码指引（品牌条） */}
      {isPending && (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: 12,
            padding: '4px 24px 8px',
            animation: 'slideUp 0.3s ease',
          }}
        >
          <div
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              padding: '6px 12px',
              borderRadius: 999,
              background: 'rgba(7, 193, 96, 0.08)',
              color: '#07C160',
              fontSize: 12,
              fontWeight: 600,
            }}
          >
            <SiWechat size={14} />
            {t('微信')}
          </div>
          <span style={{ color: 'var(--semi-color-text-3)', fontSize: 12 }}>
            {t('或')}
          </span>
          <div
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              padding: '6px 12px',
              borderRadius: 999,
              background: 'rgba(22, 119, 255, 0.08)',
              color: '#1677FF',
              fontSize: 12,
              fontWeight: 600,
            }}
          >
            <SiAlipay size={14} />
            {t('支付宝')}
          </div>
        </div>
      )}

      {/* 订单号 */}
      <div
        style={{
          textAlign: 'center',
          fontSize: 11,
          color: 'var(--semi-color-text-3)',
          padding: '0 24px 14px',
          wordBreak: 'break-all',
        }}
      >
        {t('订单号')}：{tradeNo || '-'}
      </div>

      {/* 操作按钮 */}
      <div
        style={{
          display: 'flex',
          gap: 10,
          padding: '0 20px 20px',
        }}
      >
        {isPending && (
          <Button
            block
            type='primary'
            theme='light'
            icon={<RefreshCw size={14} />}
            loading={checking}
            onClick={() => fetchOrder()}
          >
            {t('我已完成支付')}
          </Button>
        )}
        {isExpired && (
          <Button
            block
            type='primary'
            theme='solid'
            onClick={handleClose}
          >
            {t('重新发起')}
          </Button>
        )}
        <Button
          block
          theme='borderless'
          type='tertiary'
          onClick={handleClose}
        >
          {isSuccess ? t('关闭') : t('取消')}
        </Button>
      </div>
    </Modal>
  );
};

export default IPayNowQRModal;
