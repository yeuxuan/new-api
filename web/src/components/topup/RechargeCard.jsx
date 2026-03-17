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
import {
  Typography,
  Tag,
  Card,
  Button,
  Banner,
  Skeleton,
  Form,
  Space,
  Row,
  Col,
  Spin,
  Tooltip,
  Tabs,
  TabPane,
} from '@douyinfe/semi-ui';
import { SiAlipay, SiWechat, SiStripe } from 'react-icons/si';
import {
  CreditCard,
  Coins,
  Wallet,
  BarChart2,
  TrendingUp,
  Receipt,
  Sparkles,
  Flame,
  ArrowDown,
  Gift,
  Zap,
} from 'lucide-react';
import { IconGift } from '@douyinfe/semi-icons';
import { useMinimumLoadingTime } from '../../hooks/common/useMinimumLoadingTime';
import { getCurrencyConfig } from '../../helpers/render';
import SubscriptionPlansCard from './SubscriptionPlansCard';

const { Text } = Typography;

const RechargeCard = ({
  t,
  enableOnlineTopUp,
  enableStripeTopUp,
  enableCreemTopUp,
  creemProducts,
  creemPreTopUp,
  presetAmounts,
  selectedPreset,
  selectPresetAmount,
  formatLargeNumber,
  priceRatio,
  topUpCount,
  minTopUp,
  renderQuotaWithAmount,
  getAmount,
  setTopUpCount,
  setSelectedPreset,
  renderAmount,
  amountLoading,
  payMethods,
  preTopUp,
  paymentLoading,
  payWay,
  redemptionCode,
  setRedemptionCode,
  topUp,
  isSubmitting,
  topUpLink,
  openTopUpLink,
  userState,
  renderQuota,
  statusLoading,
  topupInfo,
  topupGroupRatio = 1,
  onOpenHistory,
  subscriptionLoading = false,
  subscriptionPlans = [],
  billingPreference,
  onChangeBillingPreference,
  activeSubscriptions = [],
  allSubscriptions = [],
  reloadSubscriptionSelf,
}) => {
  const onlineFormApiRef = useRef(null);
  const redeemFormApiRef = useRef(null);
  const initialTabSetRef = useRef(false);
  const showAmountSkeleton = useMinimumLoadingTime(amountLoading);
  const [activeTab, setActiveTab] = useState('topup');
  const shouldShowSubscription =
    !subscriptionLoading && subscriptionPlans.length > 0;

  useEffect(() => {
    if (initialTabSetRef.current) return;
    if (subscriptionLoading) return;
    setActiveTab(shouldShowSubscription ? 'subscription' : 'topup');
    initialTabSetRef.current = true;
  }, [shouldShowSubscription, subscriptionLoading]);

  useEffect(() => {
    if (!shouldShowSubscription && activeTab !== 'topup') {
      setActiveTab('topup');
    }
  }, [shouldShowSubscription, activeTab]);
  const topupContent = (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, width: '100%' }}>
      {/* 统计数据 Banner */}
      <div
        style={{
          borderRadius: 16,
          overflow: 'hidden',
          background: 'linear-gradient(135deg, #1a56db 0%, #0e3fa8 60%, #092a80 100%)',
          position: 'relative',
        }}
      >
        {/* 装饰纹理 */}
        <div
          style={{
            position: 'absolute',
            inset: 0,
            backgroundImage: `url('/cover-4.webp')`,
            backgroundSize: 'cover',
            backgroundPosition: 'center',
            opacity: 0.12,
          }}
        />
        {/* 右侧大圆装饰 */}
        <div
          style={{
            position: 'absolute',
            right: -40,
            top: -40,
            width: 160,
            height: 160,
            borderRadius: '50%',
            background: 'rgba(255,255,255,0.06)',
            pointerEvents: 'none',
          }}
        />
        <div
          style={{
            position: 'absolute',
            right: 40,
            bottom: -30,
            width: 100,
            height: 100,
            borderRadius: '50%',
            background: 'rgba(255,255,255,0.04)',
            pointerEvents: 'none',
          }}
        />

        <div style={{ position: 'relative', padding: '20px 24px 20px' }}>
          <div
            style={{
              fontSize: 11,
              fontWeight: 700,
              color: 'rgba(255,255,255,0.55)',
              letterSpacing: 1.5,
              textTransform: 'uppercase',
              marginBottom: 16,
            }}
          >
            {t('账户统计')}
          </div>

          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(3, 1fr)',
              gap: 8,
            }}
          >
            {[
              {
                icon: <Wallet size={13} />,
                label: t('当前余额'),
                value: renderQuota(userState?.user?.quota),
              },
              {
                icon: <TrendingUp size={13} />,
                label: t('历史消耗'),
                value: renderQuota(userState?.user?.used_quota),
              },
              {
                icon: <BarChart2 size={13} />,
                label: t('请求次数'),
                value: userState?.user?.request_count || 0,
              },
            ].map((item, i) => (
              <div
                key={i}
                style={{
                  background: 'rgba(255,255,255,0.1)',
                  borderRadius: 10,
                  padding: '10px 8px',
                  backdropFilter: 'blur(4px)',
                  border: '1px solid rgba(255,255,255,0.12)',
                  textAlign: 'center',
                }}
              >
                <div
                  style={{
                    fontSize: 'clamp(14px, 3vw, 20px)',
                    fontWeight: 800,
                    color: '#fff',
                    lineHeight: 1.2,
                    marginBottom: 6,
                    letterSpacing: -0.5,
                  }}
                >
                  {item.value}
                </div>
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: 3,
                    color: 'rgba(255,255,255,0.65)',
                    fontSize: 11,
                  }}
                >
                  {item.icon}
                  {item.label}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* 在线充值表单 */}
      <Card className='!rounded-xl w-full' bodyStyle={{ padding: '16px 20px' }}>
        {statusLoading ? (
          <div className='py-8 flex justify-center'>
            <Spin size='large' />
          </div>
        ) : enableOnlineTopUp || enableStripeTopUp || enableCreemTopUp ? (
          <Form
            getFormApi={(api) => (onlineFormApiRef.current = api)}
            initValues={{ topUpCount: topUpCount }}
          >
            <div className='space-y-6'>
              {(enableOnlineTopUp || enableStripeTopUp) && (
                <Row gutter={12}>
                  <Col xs={24} sm={24} md={24} lg={10} xl={10}>
                    <Form.InputNumber
                      field='topUpCount'
                      label={t('充值数量')}
                      disabled={!enableOnlineTopUp && !enableStripeTopUp}
                      placeholder={
                        t('充值数量，最低 ') + renderQuotaWithAmount(minTopUp)
                      }
                      value={topUpCount}
                      min={minTopUp}
                      max={999999999}
                      step={1}
                      precision={0}
                      onChange={async (value) => {
                        if (value && value >= 1) {
                          setTopUpCount(value);
                          setSelectedPreset(null);
                          await getAmount(value);
                        }
                      }}
                      onBlur={(e) => {
                        const value = parseInt(e.target.value);
                        if (!value || value < 1) {
                          setTopUpCount(1);
                          getAmount(1);
                        }
                      }}
                      formatter={(value) => (value ? `${value}` : '')}
                      parser={(value) =>
                        value ? parseInt(value.replace(/[^\d]/g, '')) : 0
                      }
                      extraText={
                        <Skeleton
                          loading={showAmountSkeleton}
                          active
                          placeholder={
                            <Skeleton.Title
                              style={{
                                width: 120,
                                height: 20,
                                borderRadius: 6,
                              }}
                            />
                          }
                        >
                          <Text type='secondary' className='text-red-600'>
                            {t('实付金额：')}
                            <span style={{ color: 'red' }}>
                              {renderAmount()}
                            </span>
                          </Text>
                        </Skeleton>
                      }
                      style={{ width: '100%' }}
                    />
                  </Col>
                  <Col xs={24} sm={24} md={24} lg={14} xl={14}>
                    <Form.Slot label={t('选择支付方式')}>
                      {payMethods && payMethods.length > 0 ? (
                        <Space wrap>
                          {payMethods.map((payMethod) => {
                            const minTopupVal = Number(payMethod.min_topup) || 0;
                            const isStripe = payMethod.type === 'stripe';
                            const disabled =
                              (!enableOnlineTopUp && !isStripe) ||
                              (!enableStripeTopUp && isStripe) ||
                              minTopupVal > Number(topUpCount || 0);

                            const buttonEl = (
                              <Button
                                key={payMethod.type}
                                theme='outline'
                                type='tertiary'
                                onClick={() => preTopUp(payMethod.type)}
                                disabled={disabled}
                                loading={
                                  paymentLoading && payWay === payMethod.type
                                }
                                icon={
                                  payMethod.type === 'alipay' ? (
                                    <SiAlipay size={18} color='#1677FF' />
                                  ) : payMethod.type === 'wxpay' ? (
                                    <SiWechat size={18} color='#07C160' />
                                  ) : payMethod.type === 'stripe' ? (
                                    <SiStripe size={18} color='#635BFF' />
                                  ) : (
                                    <CreditCard
                                      size={18}
                                      color={
                                        payMethod.color ||
                                        'var(--semi-color-text-2)'
                                      }
                                    />
                                  )
                                }
                                className='!rounded-lg !px-4 !py-2'
                              >
                                {payMethod.name}
                              </Button>
                            );

                            return disabled &&
                              minTopupVal > Number(topUpCount || 0) ? (
                              <Tooltip
                                content={
                                  t('此支付方式最低充值金额为') +
                                  ' ' +
                                  minTopupVal
                                }
                                key={payMethod.type}
                              >
                                {buttonEl}
                              </Tooltip>
                            ) : (
                              <React.Fragment key={payMethod.type}>
                                {buttonEl}
                              </React.Fragment>
                            );
                          })}
                        </Space>
                      ) : (
                        <div className='text-gray-500 text-sm p-3 bg-gray-50 rounded-lg border border-dashed border-gray-300'>
                          {t('暂无可用的支付方式，请联系管理员配置')}
                        </div>
                      )}
                    </Form.Slot>
                  </Col>
                </Row>
              )}

              {(enableOnlineTopUp || enableStripeTopUp) && (
                <Form.Slot
                  label={
                    <div className='flex items-center gap-2'>
                      <span>{t('选择充值额度')}</span>
                      {(() => {
                        const { symbol, rate, type } = getCurrencyConfig();
                        if (type === 'USD') return null;

                        return (
                          <span
                            style={{
                              color: 'var(--semi-color-text-2)',
                              fontSize: '12px',
                              fontWeight: 'normal',
                            }}
                          >
                            (1 $ = {rate.toFixed(2)} {symbol})
                          </span>
                        );
                      })()}
                    </div>
                  }
                >
                  {(() => {
                    const { symbol, rate, type } = getCurrencyConfig();
                    let usdRate = 7;
                    try {
                      const s = JSON.parse(localStorage.getItem('status') || '{}');
                      usdRate = s?.usd_exchange_rate || 7;
                    } catch (e) {}
                    const snapToInt = (val) => {
                      const rounded = Math.round(val);
                      return rounded > 0 &&
                        Math.abs(val - rounded) / rounded <= 0.03
                        ? rounded
                        : parseFloat(val.toFixed(2));
                    };
                    const hotIndex =
                      presetAmounts.length >= 3 ? 2 : -1;
                    return (
                  <div className='grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3'>
                    {presetAmounts.map((preset, index) => {
                      const discount =
                        preset.discount || topupInfo?.discount?.[preset.value] || 1.0;
                      const originalPrice = preset.value * priceRatio;
                      const discountedPrice = originalPrice * discount;
                      const hasDiscount = discount < 1.0;
                      const actualPay = discountedPrice;
                      const save = originalPrice - discountedPrice;

                      let displayValue = preset.value;
                      let displayActualPay = preset.value * discount; // 实付 = 面值（整数）
                      let displaySave = preset.value - displayActualPay;

                      if (type === 'CNY') {
                        displayValue = preset.value * usdRate;
                        displaySave = displaySave * usdRate;
                        displayActualPay = preset.value * discount * usdRate;
                      } else if (type === 'CUSTOM') {
                        displayValue = preset.value * rate;
                        displayActualPay = preset.value * discount * rate;
                        displaySave = displaySave * rate;
                      }

                      displayActualPay = snapToInt(displayActualPay);
                      displaySave = snapToInt(displaySave);
                      displayValue = snapToInt(displayValue * (topupGroupRatio || 1));

                      const snappedMultiplier = snapToInt(
                        displayActualPay > 0 ? displayValue / displayActualPay : 0,
                      );
                      const multiplierText = Number.isInteger(snappedMultiplier)
                        ? snappedMultiplier
                        : snappedMultiplier.toFixed(1);
                      const showMultiplier = snappedMultiplier >= 1.05;
                      const isSelected = selectedPreset === preset.value;
                      const isHot = index === hotIndex;

                      return (
                        <div
                          key={index}
                          onClick={() => {
                            selectPresetAmount(preset);
                            onlineFormApiRef.current?.setValue(
                              'topUpCount',
                              preset.value,
                            );
                          }}
                          style={{
                            position: 'relative',
                            cursor: 'pointer',
                            borderRadius: 16,
                            overflow: 'visible',
                            transition: 'transform 0.18s ease',
                          }}
                          onMouseEnter={(e) =>
                            (e.currentTarget.style.transform = 'translateY(-2px)')
                          }
                          onMouseLeave={(e) =>
                            (e.currentTarget.style.transform = 'translateY(0)')
                          }
                        >
                          {/* 热销角标 */}
                          {isHot && (
                            <div
                              style={{
                                position: 'absolute',
                                top: -10,
                                right: -6,
                                zIndex: 10,
                                background:
                                  'linear-gradient(135deg, #ff6b35, #f7931e)',
                                color: '#fff',
                                fontSize: 10,
                                fontWeight: 800,
                                padding: '2px 7px 2px 5px',
                                borderRadius: '10px 10px 10px 2px',
                                display: 'flex',
                                alignItems: 'center',
                                gap: 2,
                                boxShadow: '0 2px 8px rgba(255,107,53,0.45)',
                                letterSpacing: 0.3,
                              }}
                            >
                              <Flame size={9} />
                              {t('热门')}
                            </div>
                          )}

                          {/* 卡片主体 */}
                          <div
                            style={{
                              borderRadius: 16,
                              border: isSelected
                                ? '2px solid var(--semi-color-primary)'
                                : '1.5px solid var(--semi-color-border)',
                              background: isSelected
                                ? 'var(--semi-color-primary-light-default)'
                                : 'var(--semi-color-bg-2)',
                              padding: '14px 10px 12px',
                              textAlign: 'center',
                              transition:
                                'border-color 0.18s, background 0.18s, box-shadow 0.18s',
                              boxShadow: isSelected
                                ? '0 0 0 3px var(--semi-color-primary-light-hover), 0 4px 16px rgba(0,0,0,0.08)'
                                : '0 1px 4px rgba(0,0,0,0.06)',
                            }}
                          >
                            {/* 倍率徽章 */}
                            {showMultiplier && (
                              <div
                                style={{
                                  display: 'inline-flex',
                                  alignItems: 'center',
                                  gap: 3,
                                  background: isSelected
                                    ? 'var(--semi-color-primary)'
                                    : 'var(--semi-color-primary-light-hover)',
                                  color: isSelected
                                    ? '#fff'
                                    : 'var(--semi-color-primary)',
                                  padding: '2px 9px',
                                  borderRadius: 20,
                                  fontSize: 11,
                                  fontWeight: 800,
                                  marginBottom: 10,
                                  letterSpacing: 0.2,
                                  transition: 'background 0.18s, color 0.18s',
                                }}
                              >
                                <Zap size={10} />
                                {multiplierText}x&nbsp;{t('充值倍率')}
                              </div>
                            )}

                            {/* 实付金额行 */}
                            <div
                              style={{
                                fontSize: 10,
                                fontWeight: 600,
                                color: 'var(--semi-color-text-2)',
                                textTransform: 'uppercase',
                                letterSpacing: 0.8,
                                marginBottom: 2,
                              }}
                            >
                              {t('实付金额')}
                            </div>
                            <div
                              style={{
                                fontSize: 20,
                                fontWeight: 800,
                                color: isSelected
                                  ? 'var(--semi-color-primary)'
                                  : 'var(--semi-color-text-0)',
                                lineHeight: 1.2,
                                letterSpacing: -0.5,
                                transition: 'color 0.18s',
                              }}
                            >
                              {symbol}
                              {Number.isInteger(displayActualPay)
                                ? displayActualPay
                                : displayActualPay.toFixed(2)}
                            </div>

                            {/* 箭头分隔 */}
                            <div
                              style={{
                                margin: '7px 0 5px',
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                gap: 4,
                              }}
                            >
                              <div
                                style={{
                                  flex: 1,
                                  height: 1,
                                  background:
                                    'var(--semi-color-border)',
                                  borderRadius: 1,
                                }}
                              />
                              <ArrowDown
                                size={12}
                                style={{
                                  color: isSelected
                                    ? 'var(--semi-color-primary)'
                                    : 'var(--semi-color-text-3)',
                                  flexShrink: 0,
                                  transition: 'color 0.18s',
                                }}
                              />
                              <div
                                style={{
                                  flex: 1,
                                  height: 1,
                                  background: 'var(--semi-color-border)',
                                  borderRadius: 1,
                                }}
                              />
                            </div>

                            {/* 平台到账行 */}
                            <div
                              style={{
                                fontSize: 10,
                                fontWeight: 600,
                                color: 'var(--semi-color-text-2)',
                                textTransform: 'uppercase',
                                letterSpacing: 0.8,
                                marginBottom: 3,
                              }}
                            >
                              {t('平台到账')}
                            </div>
                            <div
                              style={{
                                fontSize: 24,
                                fontWeight: 900,
                                color: 'var(--semi-color-primary)',
                                lineHeight: 1.1,
                                letterSpacing: -1,
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                gap: 3,
                              }}
                            >
                              <Coins size={15} style={{ flexShrink: 0 }} />
                              {formatLargeNumber(displayValue)}&nbsp;{symbol}
                            </div>

                            {/* 折扣标签 */}
                            {hasDiscount && (
                              <div
                                style={{
                                  marginTop: 8,
                                  display: 'flex',
                                  alignItems: 'center',
                                  justifyContent: 'center',
                                  gap: 5,
                                  flexWrap: 'wrap',
                                }}
                              >
                                <Tag color='green' size='small' style={{ fontWeight: 700 }}>
                                  {t('折').includes('off')
                                    ? `${((1 - parseFloat(discount)) * 100).toFixed(0)}% off`
                                    : `${(discount * 10).toFixed(1)}${t('折')}`}
                                </Tag>
                                <span
                                  style={{
                                    fontSize: 10,
                                    color: 'var(--semi-color-success)',
                                    fontWeight: 600,
                                  }}
                                >
                                  -{symbol}
                                  {Number.isInteger(displaySave)
                                    ? displaySave
                                    : displaySave.toFixed(2)}
                                </span>
                              </div>
                            )}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                    );
                  })()}
                </Form.Slot>
              )}

              {/* Creem 充值区域 */}
              {enableCreemTopUp && creemProducts.length > 0 && (
                <Form.Slot label={t('Creem 充值')}>
                  <div className='grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3'>
                    {creemProducts.map((product, index) => (
                      <Card
                        key={index}
                        onClick={() => creemPreTopUp(product)}
                        className='cursor-pointer !rounded-2xl transition-all hover:shadow-md border-gray-200 hover:border-gray-300'
                        bodyStyle={{ textAlign: 'center', padding: '16px' }}
                      >
                        <div className='font-medium text-lg mb-2'>
                          {product.name}
                        </div>
                        <div className='text-sm text-gray-600 mb-2'>
                          {t('充值额度')}: {product.quota}
                        </div>
                        <div className='text-lg font-semibold text-blue-600'>
                          {product.currency === 'EUR' ? '€' : '$'}
                          {product.price}
                        </div>
                      </Card>
                    ))}
                  </div>
                </Form.Slot>
              )}
            </div>
          </Form>
        ) : (
          <Banner
            type='info'
            description={t(
              '管理员未开启在线充值功能，请联系管理员开启或使用兑换码充值。',
            )}
            className='!rounded-xl'
            closeIcon={null}
          />
        )}
      </Card>

      {/* 兑换码充值 */}
      <div
        style={{
          borderRadius: 14,
          border: '1.5px dashed var(--semi-color-border)',
          background: 'var(--semi-color-bg-1)',
          padding: '16px 20px',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            marginBottom: 12,
            fontSize: 13,
            fontWeight: 700,
            color: 'var(--semi-color-text-1)',
          }}
        >
          <Gift size={14} style={{ color: 'var(--semi-color-primary)' }} />
          {t('兑换码充值')}
        </div>
        <Form
          getFormApi={(api) => (redeemFormApiRef.current = api)}
          initValues={{ redemptionCode: redemptionCode }}
        >
          <Form.Input
            field='redemptionCode'
            noLabel={true}
            placeholder={t('请输入兑换码')}
            value={redemptionCode}
            onChange={(value) => setRedemptionCode(value)}
            prefix={<IconGift />}
            suffix={
              <div className='flex items-center gap-2'>
                <Button
                  type='primary'
                  theme='solid'
                  onClick={topUp}
                  loading={isSubmitting}
                >
                  {t('兑换额度')}
                </Button>
              </div>
            }
            showClear
            style={{ width: '100%' }}
            extraText={
              topUpLink && (
                <Text type='tertiary'>
                  {t('在找兑换码？')}
                  <Text
                    type='secondary'
                    underline
                    className='cursor-pointer'
                    onClick={openTopUpLink}
                  >
                    {t('购买兑换码')}
                  </Text>
                </Text>
              )
            }
          />
        </Form>
      </div>
    </div>
  );

  return (
    <Card className='!rounded-2xl shadow-sm border-0'>
      {/* 卡片头部 */}
      <div className='flex items-center justify-between mb-4'>
        <div className='flex items-center gap-3'>
          <div
            style={{
              width: 36,
              height: 36,
              borderRadius: 10,
              background: 'linear-gradient(135deg, #1a56db, #0e3fa8)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              boxShadow: '0 2px 8px rgba(26,86,219,0.35)',
            }}
          >
            <CreditCard size={16} color='#fff' />
          </div>
          <div>
            <Typography.Text strong style={{ fontSize: 15 }}>
              {t('账户充值')}
            </Typography.Text>
            <div style={{ fontSize: 11, color: 'var(--semi-color-text-2)', marginTop: 1 }}>
              {t('多种充值方式，安全便捷')}
            </div>
          </div>
        </div>
        <Button
          icon={<Receipt size={15} />}
          theme='light'
          type='tertiary'
          onClick={onOpenHistory}
          style={{ borderRadius: 8 }}
        >
          {t('账单')}
        </Button>
      </div>

      {shouldShowSubscription ? (
        <Tabs type='card' activeKey={activeTab} onChange={setActiveTab}>
          <TabPane
            tab={
              <div className='flex items-center gap-2'>
                <Sparkles size={16} />
                {t('订阅套餐')}
              </div>
            }
            itemKey='subscription'
          >
            <div className='py-2'>
              <SubscriptionPlansCard
                t={t}
                loading={subscriptionLoading}
                plans={subscriptionPlans}
                payMethods={payMethods}
                enableOnlineTopUp={enableOnlineTopUp}
                enableStripeTopUp={enableStripeTopUp}
                enableCreemTopUp={enableCreemTopUp}
                billingPreference={billingPreference}
                onChangeBillingPreference={onChangeBillingPreference}
                activeSubscriptions={activeSubscriptions}
                allSubscriptions={allSubscriptions}
                reloadSubscriptionSelf={reloadSubscriptionSelf}
                withCard={false}
              />
            </div>
          </TabPane>
          <TabPane
            tab={
              <div className='flex items-center gap-2'>
                <Wallet size={16} />
                {t('额度充值')}
              </div>
            }
            itemKey='topup'
          >
            <div className='py-2'>{topupContent}</div>
          </TabPane>
        </Tabs>
      ) : (
        topupContent
      )}
    </Card>
  );
};

export default RechargeCard;
