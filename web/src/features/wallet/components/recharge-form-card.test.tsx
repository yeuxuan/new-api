/*
Copyright (C) 2023-2026 QuantumNous

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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import { subscriptionPlanSchema } from '@/features/subscriptions/types'
import { api } from '@/lib/api'
import { useSystemConfigStore } from '@/stores/system-config-store'

import type { CreemProduct, TopupInfo } from '../types'
import { RechargeFormCard } from './recharge-form-card'
import { SubscriptionPlansCard } from './subscription-plans-card'

const hiddenTopupInfo: TopupInfo = {
  hide_online_topup: true,
  enable_online_topup: true,
  enable_stripe_topup: true,
  enable_ipaynow_topup: true,
  enable_creem_topup: true,
  pay_methods: [
    { name: 'Stripe', type: 'stripe', min_topup: 1 },
    { name: 'iPayNow', type: 'ipaynow', min_topup: 1 },
  ],
  min_topup: 1,
  stripe_min_topup: 1,
  amount_options: [10],
  discount: {},
  enable_redemption: true,
}

test.each([7, 6])(
  'subscription balance cost uses decimal rounding with wallet quota=%s',
  async (quota) => {
    const { config, setConfig } = useSystemConfigStore.getState()
    setConfig({ currency: { ...config.currency, quotaPerUnit: 100 } })
    const client = new QueryClient()
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true } })
    const onPurchaseSuccess = vi.fn()
    const view = render(
      <QueryClientProvider client={client}>
        <SubscriptionPurchaseDialog
          open
          onOpenChange={vi.fn()}
          userQuota={quota}
          onPurchaseSuccess={onPurchaseSuccess}
          plan={{
            plan: subscriptionPlanSchema.parse({
              id: 1,
              title: 'Decimal plan',
              price_amount: 0.07,
              duration_unit: 'month',
              duration_value: 1,
              quota_reset_period: 'never',
              enabled: true,
              sort_order: 0,
              max_purchase_per_user: 0,
              total_amount: 0,
            }),
          }}
        />
      </QueryClientProvider>
    )
    try {
      const button = screen.getByRole('button', { name: 'Pay with Balance' })
      if (quota === 7) {
        expect(button).toBeEnabled()
        await userEvent.click(button)
        await waitFor(() =>
          expect(post).toHaveBeenCalledWith('/api/subscription/balance/pay', {
            plan_id: 1,
          })
        )
        expect(onPurchaseSuccess).toHaveBeenCalledTimes(1)
      } else {
        expect(button).toBeDisabled()
        await userEvent.click(button)
        expect(post).not.toHaveBeenCalled()
      }
    } finally {
      view.unmount()
      setConfig(config)
      client.clear()
    }
  }
)

test.each([true, false, undefined])(
  'preserves Creem and redemption with hide_online_topup=%s',
  async (hideOnlineTopup) => {
    const user = userEvent.setup()
    const onCreemProductSelect = vi.fn()
    const product: CreemProduct = {
      productId: 'prod_test',
      name: 'Creem credit pack',
      price: 10,
      quota: 10,
      currency: 'USD',
    }
    render(
      <RechargeFormCard
        topupInfo={{ ...hiddenTopupInfo, hide_online_topup: hideOnlineTopup }}
        presetAmounts={[{ value: 10, discount: 1 }]}
        selectedPreset={null}
        onSelectPreset={vi.fn()}
        topupAmount={10}
        onTopupAmountChange={vi.fn()}
        paymentAmount={70}
        calculating={false}
        onPaymentMethodSelect={vi.fn()}
        paymentLoading={null}
        redemptionCode=''
        onRedemptionCodeChange={vi.fn()}
        onRedeem={vi.fn()}
        redeeming={false}
        enableCreemTopup
        creemProducts={[product]}
        onCreemProductSelect={onCreemProductSelect}
      />
    )

    if (hideOnlineTopup) {
      expect(screen.queryByLabelText('Custom Amount')).toBeNull()
      expect(screen.queryByRole('button', { name: 'Stripe' })).toBeNull()
      expect(screen.queryByRole('button', { name: 'iPayNow' })).toBeNull()
    } else {
      expect(screen.getByLabelText('Custom Amount')).toBeVisible()
      expect(screen.getByRole('button', { name: 'Stripe' })).toBeVisible()
      expect(screen.getByRole('button', { name: 'iPayNow' })).toBeVisible()
    }
    expect(screen.getByLabelText('Have a Code?')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Redeem' })).toBeInTheDocument()
    expect(screen.getByText('Creem Payment')).toBeVisible()
    await user.click(screen.getByText(product.name))
    expect(onCreemProductSelect).toHaveBeenCalledExactlyOnceWith(product)
  }
)

test.each([true, false])(
  'subscription checkout ignores the recharge catalog and excludes iPayNow (hidden=%s)',
  async (hideOnlineTopup) => {
    const plan = subscriptionPlanSchema.parse({
      id: 1,
      title: 'Monthly plan',
      price_amount: 10,
      duration_unit: 'month',
      duration_value: 1,
      quota_reset_period: 'never',
      enabled: true,
      sort_order: 0,
      max_purchase_per_user: 0,
      total_amount: 0,
      stripe_price_id: 'price_1',
      creem_product_id: 'prod_1',
      waffo_pancake_product_id: 'waffo_1',
    })
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data:
          url === '/api/subscription/plans'
            ? [{ plan }]
            : {
                billing_preference: 'subscription_first',
                subscriptions: [],
                all_subscriptions: [],
              },
      },
    }))
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        message: 'success',
        data: { checkout_url: 'https://checkout.example.test/creem' },
      },
    })
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <SubscriptionPlansCard
          topupInfo={{
            ...hiddenTopupInfo,
            hide_online_topup: hideOnlineTopup,
            enable_stripe_topup: false,
            enable_creem_topup: false,
            enable_creem_subscription: true,
          }}
        />
      </QueryClientProvider>
    )
    await userEvent.click(
      await screen.findByRole('button', { name: 'Subscribe Now' })
    )
    const dialog = within(
      await screen.findByRole('dialog', { name: 'Purchase Subscription' })
    )
    expect(
      dialog.queryByRole('button', { name: /Stripe/ })
    ).not.toBeInTheDocument()
    expect(
      dialog.queryByRole('button', { name: /Waffo/ })
    ).not.toBeInTheDocument()
    expect(dialog.queryByRole('combobox')).not.toBeInTheDocument()
    expect(dialog.queryByText('iPayNow')).not.toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: /Creem/ }))
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api/subscription/creem/pay', {
        plan_id: 1,
      })
    )
    expect(open).toHaveBeenCalledWith(
      'https://checkout.example.test/creem',
      '_blank'
    )
    client.clear()
  }
)
