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
import { expect, test, vi } from 'vitest'

import type { TopupInfo } from '../types'
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

test('hides direct payment controls while keeping redemption available', () => {
  render(
    <RechargeFormCard
      topupInfo={hiddenTopupInfo}
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
    />
  )

  expect(screen.queryByLabelText('Custom Amount')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Stripe' })).toBeNull()
  expect(screen.getByLabelText('Have a Code?')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Redeem' })).toBeInTheDocument()
})
