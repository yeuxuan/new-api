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
import { act, renderHook } from '@testing-library/react'
import { toast } from 'sonner'
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { PAYMENT_TYPES } from '../constants'
import { useIPayNowPayment } from './use-ipaynow-payment'
import { requestPaymentAmount } from './use-payment'

describe('payment amount routing', () => {
  test('uses the dedicated Waffo amount calculator', async () => {
    const calls: string[] = []
    const amount = await requestPaymentAmount(120, PAYMENT_TYPES.WAFFO, {
      regular: async () => {
        calls.push('regular')
        return { success: true, data: '1' }
      },
      stripe: async () => {
        calls.push('stripe')
        return { success: true, data: '2' }
      },
      waffo: async (request) => {
        calls.push(`waffo:${request.amount}`)
        return { success: true, data: '18.75' }
      },
      waffoPancake: async () => {
        calls.push('pancake')
        return { success: true, data: '4' }
      },
    })

    expect(amount).toBe(18.75)
    expect(calls).toEqual(['waffo:120'])
  })

  test('uses the dedicated iPayNow amount calculator', async () => {
    const calls: string[] = []
    const amount = await requestPaymentAmount(80, PAYMENT_TYPES.IPAYNOW, {
      regular: async () => {
        calls.push('regular')
        return { success: true, data: '1' }
      },
      stripe: async () => ({ success: true, data: '2' }),
      ipaynow: async (request) => {
        calls.push(`ipaynow:${request.amount}`)
        return { success: true, data: '12.5' }
      },
      waffo: async () => ({ success: true, data: '3' }),
      waffoPancake: async () => ({ success: true, data: '4' }),
    })

    expect(amount).toBe(12.5)
    expect(calls).toEqual(['ipaynow:80'])
  })
})

describe('iPayNow payment results', () => {
  test('shows the rejection reason and clears processing after business failure', async () => {
    vi.spyOn(api, 'post').mockResolvedValue({
      data: { message: 'error', data: 'Merchant rejected the order' },
    })
    const notify = vi.spyOn(toast, 'error').mockReturnValue('payment-error')
    const { result } = renderHook(() => useIPayNowPayment())

    await act(async () => {
      expect(await result.current.processIPayNowPayment(10)).toBeNull()
    })

    expect(notify).toHaveBeenCalledWith('Merchant rejected the order')
    expect(result.current.processing).toBe(false)
  })

  test('preserves the server reason after an HTTP failure', async () => {
    vi.spyOn(api, 'post').mockRejectedValue({
      isAxiosError: true,
      response: {
        data: { message: 'error', data: 'Payment provider unavailable' },
      },
    })
    const notify = vi.spyOn(toast, 'error').mockReturnValue('payment-error')
    const { result } = renderHook(() => useIPayNowPayment())

    await act(async () => {
      expect(await result.current.processIPayNowPayment(10)).toBeNull()
    })

    expect(notify).toHaveBeenCalledWith('Payment provider unavailable')
    expect(result.current.processing).toBe(false)
  })

  test('returns the QR order without an error after successful payment creation', async () => {
    vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: { trade_no: 'order-1', qr_url: 'https://pay.example/qr' },
      },
    })
    const notify = vi.spyOn(toast, 'error').mockReturnValue('payment-error')
    const { result } = renderHook(() => useIPayNowPayment())

    await act(async () => {
      expect(await result.current.processIPayNowPayment(10)).toEqual({
        tradeNo: 'order-1',
        qrUrl: 'https://pay.example/qr',
      })
    })

    expect(notify).not.toHaveBeenCalled()
    expect(result.current.processing).toBe(false)
  })
})
