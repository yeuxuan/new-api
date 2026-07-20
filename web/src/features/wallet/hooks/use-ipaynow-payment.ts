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
import i18next from 'i18next'
import { useState, useCallback } from 'react'
import { toast } from 'sonner'

import { requestIPayNowPayment, isApiSuccess } from '../api'

export interface IPayNowOrder {
  tradeNo: string
  qrUrl: string
}

/**
 * Hook for initiating iPayNow (WeChat/Alipay aggregated QR) payments.
 *
 * Unlike redirect-based gateways, iPayNow returns a QR url that is rendered
 * in-app; the caller opens a dialog that polls the order status until it is
 * paid or expires.
 */
export function useIPayNowPayment() {
  const [processing, setProcessing] = useState(false)

  const processIPayNowPayment = useCallback(
    async (amount: number): Promise<IPayNowOrder | null> => {
      setProcessing(true)
      try {
        const response = await requestIPayNowPayment({
          amount: Math.floor(amount),
          payment_method: 'ipaynow',
        })

        if (
          isApiSuccess(response) &&
          response.data?.trade_no &&
          response.data?.qr_url
        ) {
          return {
            tradeNo: response.data.trade_no,
            qrUrl: response.data.qr_url,
          }
        }

        toast.error(response.message || i18next.t('Payment request failed'))
        return null
      } catch (_error) {
        toast.error(i18next.t('Payment request failed'))
        return null
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  return { processing, processIPayNowPayment }
}
