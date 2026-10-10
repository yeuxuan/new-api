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
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react';
import RechargeCard from '../RechargeCard';
import { API } from '../../../helpers';
import { Toast } from '@douyinfe/semi-ui';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key) => key, i18n: { language: 'zh-CN' } }),
}));

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.clear();
});

const plan = {
  plan: {
    id: 1,
    title: 'Local subscription',
    price_amount: 10,
    total_amount: 100,
    duration_unit: 'month',
    duration_value: 1,
    stripe_price_id: 'stripe-test',
    creem_product_id: 'creem-test',
  },
};

function renderWallet(overrides = {}) {
  const props = {
    t: (key) => key,
    enableOnlineTopUp: true,
    enableStripeTopUp: true,
    enableCreemTopUp: true,
    topupInfo: {
      enable_creem_subscription: true,
      payment_compliance_confirmed: true,
    },
    userState: { user: { quota: 5000000 } },
    creemProducts: [
      { productId: 'creem-test', name: 'CREEM package', quota: 100, price: 10 },
    ],
    creemPreTopUp: vi.fn(),
    presetAmounts: [],
    payMethods: [{ type: 'alipay', name: 'Alipay' }],
    renderQuota: String,
    renderQuotaWithAmount: String,
    renderAmount: () => '10',
    formatLargeNumber: String,
    topUpCount: 10,
    minTopUp: 1,
    getAmount: vi.fn(),
    priceRatio: 1,
    enableRedemption: false,
    billingPreference: 'subscription_first',
    subscriptionPlans: [plan],
    ...overrides,
  };
  render(<RechargeCard {...props} />);
  return props;
}

describe('wallet payment visibility', () => {
  it.each([true, false])(
    'keeps CREEM for subscriptions when hidden=%s',
    async (hideOnlineTopup) => {
      renderWallet({ hideOnlineTopup });
      fireEvent.click(await screen.findByRole('button', { name: /立即订阅/ }));
      expect(
        await screen.findByRole('button', { name: /Creem/ }),
      ).toBeVisible();
      const stripe = screen.queryByRole('button', { name: /Stripe/ });
      const epay = screen.queryByRole('button', { name: '支付' });
      if (hideOnlineTopup) {
        expect(stripe).not.toBeInTheDocument();
        expect(epay).not.toBeInTheDocument();
      } else {
        expect(stripe).toBeVisible();
        expect(epay).toBeVisible();
      }
    },
  );

  it('keeps CREEM recharge usable while hiding ordinary recharge', () => {
    const props = renderWallet({
      hideOnlineTopup: true,
      subscriptionPlans: [],
    });
    expect(screen.getByText('Creem 充值')).toBeVisible();
    expect(screen.queryByText('充值数量')).not.toBeInTheDocument();
    expect(screen.queryByText('付款方式')).not.toBeInTheDocument();
    fireEvent.click(screen.getByText('CREEM package'));
    expect(props.creemPreTopUp).toHaveBeenCalledWith(props.creemProducts[0]);
  });

  it.each([true, false])(
    'uses subscription CREEM independently and excludes iPayNow (hidden=%s)',
    async (hideOnlineTopup) => {
      const post = vi.spyOn(API, 'post').mockResolvedValue({
        data: {
          message: 'success',
          data: {
            checkout_url: 'https://checkout.example.test/subscription',
          },
        },
      });
      const open = vi.spyOn(window, 'open').mockReturnValue(null);
      renderWallet({
        hideOnlineTopup,
        enableCreemTopUp: false,
        creemProducts: [],
        payMethods: [{ type: 'ipaynow', name: 'iPayNow' }],
      });
      fireEvent.click(await screen.findByRole('button', { name: /立即订阅/ }));
      const dialog = within(await screen.findByRole('dialog'));
      expect(dialog.queryByText('iPayNow')).not.toBeInTheDocument();
      expect(
        dialog.queryByRole('button', { name: '支付' }),
      ).not.toBeInTheDocument();
      fireEvent.click(dialog.getByRole('button', { name: /Creem/ }));
      await waitFor(() =>
        expect(post).toHaveBeenCalledWith('/api/subscription/creem/pay', {
          plan_id: 1,
        }),
      );
      expect(open).toHaveBeenCalledWith(
        'https://checkout.example.test/subscription',
        '_blank',
      );
    },
  );

  it('buys a balance-only subscription and refreshes wallet and subscriptions', async () => {
    localStorage.setItem('quota_per_unit', '500000');
    const post = vi
      .spyOn(API, 'post')
      .mockResolvedValue({ data: { success: true } });
    const props = renderWallet({
      hideOnlineTopup: true,
      enableCreemTopUp: false,
      topupInfo: {
        enable_creem_subscription: false,
        payment_compliance_confirmed: true,
      },
      reloadSubscriptionSelf: vi.fn(),
      onSubscriptionPurchaseSuccess: vi.fn(),
    });
    fireEvent.click(await screen.findByRole('button', { name: /立即订阅/ }));
    fireEvent.click(
      await screen.findByRole('button', { name: 'Pay with Balance' }),
    );
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api/subscription/balance/pay', {
        plan_id: 1,
      }),
    );
    await waitFor(() =>
      expect(props.reloadSubscriptionSelf).toHaveBeenCalledTimes(1),
    );
    expect(props.onSubscriptionPurchaseSuccess).toHaveBeenCalledTimes(1);
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: 'Pay with Balance' }),
      ).not.toBeInTheDocument(),
    );
  });

  it.each([
    'insufficient wallet',
    'fractional cost rounds up',
    'balance disabled',
    'purchase limit',
    'compliance unconfirmed',
  ])('blocks balance payment: %s', async (scenario) => {
    localStorage.setItem('quota_per_unit', '500000');
    const post = vi.spyOn(API, 'post');
    const overrides = {};
    if (scenario === 'insufficient wallet')
      overrides.userState = { user: { quota: 4999999, bonus_quota: 5000000 } };
    if (scenario === 'fractional cost rounds up') {
      localStorage.setItem('quota_per_unit', '3');
      overrides.subscriptionPlans = [
        { plan: { ...plan.plan, price_amount: 1.1 } },
      ];
      overrides.userState = { user: { quota: 3 } };
    }
    if (scenario === 'balance disabled')
      overrides.subscriptionPlans = [
        { plan: { ...plan.plan, allow_balance_pay: false } },
      ];
    if (scenario === 'purchase limit') {
      overrides.subscriptionPlans = [
        { plan: { ...plan.plan, max_purchase_per_user: 1 } },
      ];
      overrides.allSubscriptions = [
        { subscription: { plan_id: 1, status: 'expired' } },
      ];
    }
    if (scenario === 'compliance unconfirmed')
      overrides.topupInfo = { payment_compliance_confirmed: false };
    renderWallet(overrides);
    const subscribe = await screen.findByRole('button', {
      name: /立即订阅|已达上限/,
    });
    if (scenario === 'purchase limit') {
      expect(subscribe).toBeDisabled();
    } else {
      fireEvent.click(subscribe);
      const balance = await screen.findByRole('button', {
        name: 'Pay with Balance',
      });
      expect(balance).toBeDisabled();
      fireEvent.click(balance);
    }
    expect(post).not.toHaveBeenCalled();
  });

  it('keeps a rejected balance purchase open without refreshing or reporting success', async () => {
    localStorage.setItem('quota_per_unit', '500000');
    const errorToast = vi.spyOn(Toast, 'error').mockImplementation(() => {});
    const post = vi.spyOn(API, 'post').mockResolvedValue({
      data: { success: false, message: 'Balance changed' },
    });
    const props = renderWallet({
      reloadSubscriptionSelf: vi.fn(),
      onSubscriptionPurchaseSuccess: vi.fn(),
    });
    fireEvent.click(await screen.findByRole('button', { name: /立即订阅/ }));
    fireEvent.click(
      await screen.findByRole('button', { name: 'Pay with Balance' }),
    );
    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
    expect(props.reloadSubscriptionSelf).not.toHaveBeenCalled();
    expect(props.onSubscriptionPurchaseSuccess).not.toHaveBeenCalled();
    expect(errorToast).toHaveBeenCalledWith('错误：Balance changed');
    expect(
      screen.getByRole('button', { name: 'Pay with Balance' }),
    ).toBeEnabled();
  });

  it.each([7, 6])(
    'uses the exact decimal balance cost with wallet quota=%s',
    async (quota) => {
      localStorage.setItem('quota_per_unit', '100');
      const post = vi
        .spyOn(API, 'post')
        .mockResolvedValue({ data: { success: true } });
      renderWallet({
        userState: { user: { quota } },
        subscriptionPlans: [{ plan: { ...plan.plan, price_amount: 0.07 } }],
      });
      fireEvent.click(await screen.findByRole('button', { name: /立即订阅/ }));
      const button = await screen.findByRole('button', {
        name: 'Pay with Balance',
      });
      if (quota === 7) {
        expect(button).toBeEnabled();
        fireEvent.click(button);
        await waitFor(() =>
          expect(post).toHaveBeenCalledWith('/api/subscription/balance/pay', {
            plan_id: 1,
          }),
        );
      } else {
        expect(button).toBeDisabled();
        fireEvent.click(button);
        expect(post).not.toHaveBeenCalled();
      }
    },
  );
});
