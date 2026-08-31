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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import type { CreemProduct, TopupInfo } from '../types'
import { RechargeFormCard } from './recharge-form-card'

const hiddenTopupInfo: TopupInfo = {
  hide_online_topup: true,
  enable_online_topup: true,
  enable_stripe_topup: true,
  pay_methods: [{ name: 'Stripe', type: 'stripe', min_topup: 1 }],
  min_topup: 1,
  stripe_min_topup: 1,
  amount_options: [10],
  discount: {},
  enable_redemption: true,
}

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
    } else {
      expect(screen.getByLabelText('Custom Amount')).toBeVisible()
      expect(screen.getByRole('button', { name: 'Stripe' })).toBeVisible()
    }
    expect(screen.getByLabelText('Have a Code?')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Redeem' })).toBeInTheDocument()
    expect(screen.getByText('Creem Payment')).toBeVisible()
    await user.click(screen.getByText(product.name))
    expect(onCreemProductSelect).toHaveBeenCalledExactlyOnceWith(product)
  }
)
