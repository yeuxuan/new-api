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
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { formatQuota } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

interface QuotaLogItem {
  id: number
  created_at: number
  type: number
  quota: number
  model_name?: string
  token_name?: string
  content?: string
}

interface UserQuotaLogsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username: string } | null
}

const PAGE_SIZE = 20

function formatTimestamp(ts?: number): string {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString()
}

function logTypeBadge(type: number, t: (k: string) => string) {
  const map: Record<number, { variant: 'default' | 'secondary' | 'destructive' | 'outline'; text: string }> = {
    1: { variant: 'secondary', text: t('Recharge') },
    2: { variant: 'default', text: t('Consume') },
    3: { variant: 'outline', text: t('Manage') },
    4: { variant: 'outline', text: t('System') },
    5: { variant: 'destructive', text: t('Error') },
    6: { variant: 'secondary', text: t('Refund') },
  }
  const info = map[type] || { variant: 'outline' as const, text: t('Unknown') }
  return <Badge variant={info.variant}>{info.text}</Badge>
}

/** Render the quota delta with sign/color, mirroring the classic behavior. */
function renderQuotaChange(item: QuotaLogItem) {
  const quota = Number(item.quota) || 0
  if (quota === 0) {
    const match = item.content?.match(/[$＄]\s*([\d.]+)/)
    if (match) {
      const from = item.content?.match(/从\s*[$＄]\s*([\d.]+)/)
      const to = item.content?.match(/修改为\s*[$＄]\s*([\d.]+)/)
      const isDeduction =
        from && to ? parseFloat(to[1]) < parseFloat(from[1]) : item.type === 2
      return (
        <span className={isDeduction ? 'text-destructive' : 'text-emerald-600'}>
          {isDeduction ? '-' : '+'}${match[1]}
        </span>
      )
    }
    return <span className='text-muted-foreground'>-</span>
  }
  const displayQuota = item.type === 2 ? -Math.abs(quota) : quota
  return (
    <span className={displayQuota < 0 ? 'text-destructive' : 'text-emerald-600'}>
      {displayQuota < 0 ? '-' : '+'}
      {formatQuota(Math.abs(quota))}
    </span>
  )
}

export function UserQuotaLogsDialog({
  open,
  onOpenChange,
  user,
}: UserQuotaLogsDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [logs, setLogs] = useState<QuotaLogItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [logType, setLogType] = useState(0)

  const loadLogs = useCallback(
    async (p: number, type: number) => {
      if (!user?.username) return
      setLoading(true)
      try {
        const params = new URLSearchParams({
          username: user.username,
          p: String(p),
          page_size: String(PAGE_SIZE),
        })
        if (type > 0) params.set('type', String(type))
        const res = await api.get(`/api/log/?${params.toString()}`)
        const body = res.data as {
          success?: boolean
          message?: string
          data?: { items?: QuotaLogItem[]; total?: number }
        }
        if (body.success) {
          setLogs(body.data?.items || [])
          setTotal(body.data?.total || 0)
        } else {
          toast.error(body.message || t('Request failed'))
        }
      } catch (_error) {
        toast.error(t('Request failed'))
      } finally {
        setLoading(false)
      }
    },
    [user?.username, t]
  )

  useEffect(() => {
    if (!open || !user?.username) return
    setPage(1)
    setLogType(0)
    loadLogs(1, 0)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, user?.username])

  const handleTypeChange = (type: number) => {
    setLogType(type)
    setPage(1)
    loadLogs(1, type)
  }

  const handlePageChange = (p: number) => {
    setPage(p)
    loadLogs(p, logType)
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side='right'
        className='flex w-full flex-col gap-0 p-0 sm:max-w-3xl'
      >
        <SheetHeader className='border-b px-4 py-3'>
          <SheetTitle className='flex items-center gap-2'>
            <Badge variant='secondary'>{t('Quota')}</Badge>
            {t('Quota Change Logs')}
          </SheetTitle>
          <SheetDescription>
            {user?.username || '-'} (ID: {user?.id ?? '-'})
          </SheetDescription>
        </SheetHeader>

        <div className='flex items-center gap-2 px-4 py-3'>
          <span className='text-sm text-muted-foreground'>{t('Type')}:</span>
          <NativeSelect
            value={String(logType)}
            onChange={(e) => handleTypeChange(Number(e.target.value))}
            className='h-8 w-32'
          >
            <NativeSelectOption value='0'>{t('All')}</NativeSelectOption>
            <NativeSelectOption value='1'>{t('Recharge')}</NativeSelectOption>
            <NativeSelectOption value='2'>{t('Consume')}</NativeSelectOption>
            <NativeSelectOption value='3'>{t('Manage')}</NativeSelectOption>
            <NativeSelectOption value='4'>{t('System')}</NativeSelectOption>
            <NativeSelectOption value='5'>{t('Error')}</NativeSelectOption>
            <NativeSelectOption value='6'>{t('Refund')}</NativeSelectOption>
          </NativeSelect>
        </div>

        <ScrollArea className='flex-1 px-4'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Time')}</TableHead>
                <TableHead>{t('Type')}</TableHead>
                <TableHead>{t('Quota Change')}</TableHead>
                <TableHead>{t('Model')}</TableHead>
                <TableHead>{t('Token')}</TableHead>
                <TableHead>{t('Details')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={6} className='py-8 text-center text-muted-foreground'>
                    {t('Loading...')}
                  </TableCell>
                </TableRow>
              ) : logs.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className='py-8 text-center text-muted-foreground'>
                    {t('No records')}
                  </TableCell>
                </TableRow>
              ) : (
                logs.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className='whitespace-nowrap text-xs'>
                      {formatTimestamp(item.created_at)}
                    </TableCell>
                    <TableCell>{logTypeBadge(item.type, t)}</TableCell>
                    <TableCell className='whitespace-nowrap text-sm font-medium'>
                      {renderQuotaChange(item)}
                    </TableCell>
                    <TableCell>
                      {item.model_name ? (
                        <Badge variant='outline'>{item.model_name}</Badge>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      {item.token_name ? (
                        <Badge variant='secondary'>{item.token_name}</Badge>
                      ) : null}
                    </TableCell>
                    <TableCell className='max-w-[260px] truncate text-xs text-muted-foreground'>
                      {item.content}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </ScrollArea>

        <div className='flex items-center justify-between border-t px-4 py-3 text-sm'>
          <span className='text-muted-foreground'>
            {t('Total')}: {total}
          </span>
          <div className='flex items-center gap-2'>
            <Button
              variant='outline'
              size='sm'
              disabled={page <= 1 || loading}
              onClick={() => handlePageChange(page - 1)}
            >
              {t('Previous')}
            </Button>
            <span className='text-muted-foreground'>
              {page} / {totalPages}
            </span>
            <Button
              variant='outline'
              size='sm'
              disabled={page >= totalPages || loading}
              onClick={() => handlePageChange(page + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}
