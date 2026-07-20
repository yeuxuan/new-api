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
import {
  AlertTriangle,
  CheckCircle2,
  Clock,
  Loader2,
  RefreshCw,
} from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SiAlipay, SiWechat } from 'react-icons/si'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import { queryIPayNowOrder, isApiSuccess } from '../../api'
import type { IPayNowOrderData } from '../../types'

const POLL_INTERVAL_MS = 3000
const POLL_TIMEOUT_MS = 5 * 60 * 1000

type OrderStatus = 'pending' | 'success' | 'expired'

function formatMMSS(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const m = String(Math.floor(total / 60)).padStart(2, '0')
  const s = String(total % 60).padStart(2, '0')
  return `${m}:${s}`
}

interface IPayNowQRDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  tradeNo?: string
  qrUrl?: string
  /** Pay money amount (CNY) to display */
  amount?: number
  /** Called when the order is confirmed paid */
  onPaid?: (data: IPayNowOrderData) => void
}

export function IPayNowQRDialog({
  open,
  onOpenChange,
  tradeNo,
  qrUrl,
  amount,
  onPaid,
}: IPayNowQRDialogProps) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<OrderStatus>('pending')
  const [checking, setChecking] = useState(false)
  const [remainingMs, setRemainingMs] = useState(POLL_TIMEOUT_MS)
  const [paidAmount, setPaidAmount] = useState<number | null>(null)

  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const countdownRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const deadlineRef = useRef(0)
  const onPaidRef = useRef(onPaid)
  onPaidRef.current = onPaid

  const stopAll = useCallback(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
    if (countdownRef.current) {
      clearInterval(countdownRef.current)
      countdownRef.current = null
    }
  }, [])

  const fetchOrder = useCallback(
    async (silent = false) => {
      if (!tradeNo) return
      if (!silent) setChecking(true)
      try {
        const response = await queryIPayNowOrder(tradeNo)
        if (isApiSuccess(response) && response.data) {
          const data = response.data
          if (data.status === 'success') {
            stopAll()
            setStatus('success')
            if (typeof data.money === 'number') setPaidAmount(data.money)
            if (!silent) toast.success(t('Payment successful'))
            onPaidRef.current?.(data)
          } else if (data.status === 'expired') {
            stopAll()
            setStatus('expired')
          }
        } else if (!silent) {
          toast.error(response.message || t('Failed to query order'))
        }
      } catch (_error) {
        if (!silent) toast.error(t('Failed to query order'))
      } finally {
        if (!silent) setChecking(false)
      }
    },
    [tradeNo, stopAll, t]
  )

  useEffect(() => {
    if (!open || !tradeNo) {
      stopAll()
      return undefined
    }
    setStatus('pending')
    setPaidAmount(null)
    deadlineRef.current = Date.now() + POLL_TIMEOUT_MS
    setRemainingMs(POLL_TIMEOUT_MS)

    const tick = async () => {
      const remain = deadlineRef.current - Date.now()
      if (remain <= 0) {
        stopAll()
        setStatus('expired')
        return
      }
      await fetchOrder(true)
    }

    tick()
    timerRef.current = setInterval(tick, POLL_INTERVAL_MS)
    countdownRef.current = setInterval(() => {
      setRemainingMs(Math.max(0, deadlineRef.current - Date.now()))
    }, 1000)

    return () => stopAll()
  }, [open, tradeNo, fetchOrder, stopAll])

  const handleClose = () => {
    stopAll()
    onOpenChange(false)
  }

  const isPending = status === 'pending'
  const isSuccess = status === 'success'
  const isExpired = status === 'expired'

  const displayAmount = (() => {
    const val = isSuccess ? (paidAmount ?? amount) : amount
    if (val === undefined || val === null) return '—'
    const num = Number(val)
    return Number.isFinite(num) ? num.toFixed(2) : '—'
  })()

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) handleClose()
      }}
    >
      <DialogContent className='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-[400px]'>
        <DialogHeader>
          <DialogTitle className='flex items-center justify-center'>
            <span
              className={
                'inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-semibold ' +
                (isSuccess
                  ? 'bg-emerald-500/10 text-emerald-600'
                  : isExpired
                    ? 'bg-red-500/10 text-red-600'
                    : 'bg-blue-500/10 text-blue-600')
              }
            >
              {isPending && <Clock className='h-3 w-3' />}
              {isSuccess && <CheckCircle2 className='h-3 w-3' />}
              {isExpired && <AlertTriangle className='h-3 w-3' />}
              {isPending && `${t('Remaining')} ${formatMMSS(remainingMs)}`}
              {isSuccess && t('Payment successful')}
              {isExpired && t('Order expired')}
            </span>
          </DialogTitle>
        </DialogHeader>

        {/* Amount */}
        <div className='text-center'>
          <div className='text-muted-foreground mb-1 text-[11px] font-semibold tracking-wide uppercase'>
            {isSuccess ? t('Amount received') : t('Amount due')}
          </div>
          <div
            className={
              'inline-flex items-baseline gap-0.5 font-extrabold tracking-tight ' +
              (isSuccess ? 'text-emerald-600' : 'text-foreground')
            }
          >
            <span className='text-lg'>¥</span>
            <span className='text-4xl leading-none'>{displayAmount}</span>
          </div>
        </div>

        {/* QR / status area */}
        <div className='flex justify-center py-2'>
          <div
            className={
              'flex h-60 w-60 items-center justify-center rounded-2xl bg-white shadow-md ' +
              (isPending
                ? 'border border-emerald-500/25'
                : isSuccess
                  ? 'border border-emerald-500/40'
                  : 'border border-red-500/25')
            }
          >
            {isPending &&
              (qrUrl ? (
                <QRCodeSVG value={qrUrl} size={200} level='H' />
              ) : (
                <Loader2 className='text-muted-foreground h-8 w-8 animate-spin' />
              ))}
            {isSuccess && (
              <CheckCircle2
                className='h-20 w-20 text-emerald-500'
                strokeWidth={2.2}
              />
            )}
            {isExpired && (
              <div className='text-muted-foreground flex flex-col items-center gap-2'>
                <AlertTriangle
                  className='h-14 w-14 text-red-500'
                  strokeWidth={2}
                />
                <span className='text-sm'>{t('QR code expired')}</span>
              </div>
            )}
          </div>
        </div>

        {/* WeChat / Alipay hint */}
        {isPending && (
          <div className='flex items-center justify-center gap-3'>
            <span className='inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 px-3 py-1.5 text-xs font-semibold text-emerald-600'>
              <SiWechat className='h-3.5 w-3.5' />
              {t('WeChat')}
            </span>
            <span className='text-muted-foreground text-xs'>{t('or')}</span>
            <span className='inline-flex items-center gap-1.5 rounded-full bg-blue-500/10 px-3 py-1.5 text-xs font-semibold text-blue-600'>
              <SiAlipay className='h-3.5 w-3.5' />
              {t('Alipay')}
            </span>
          </div>
        )}

        {/* Trade number */}
        <div className='text-muted-foreground text-center text-[11px] break-all'>
          {t('Order No.')}: {tradeNo || '-'}
        </div>

        {/* Actions */}
        <div className='flex gap-2'>
          {isPending && (
            <Button
              className='flex-1'
              variant='outline'
              disabled={checking}
              onClick={() => fetchOrder(false)}
            >
              {checking ? (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              ) : (
                <RefreshCw className='mr-2 h-4 w-4' />
              )}
              {t("I've completed the payment")}
            </Button>
          )}
          <Button
            className='flex-1'
            variant={isSuccess ? 'default' : 'ghost'}
            onClick={handleClose}
          >
            {isSuccess ? t('Close') : t('Cancel')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
