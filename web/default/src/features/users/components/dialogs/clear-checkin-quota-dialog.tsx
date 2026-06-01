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
import { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Textarea } from '@/components/ui/textarea'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

interface PreviewItem {
  user_id: number
  username: string
  checkin_quota: number
  actual_clear: number
}

interface PreviewData {
  users: PreviewItem[]
  affected_users: number
  total_actual_clear: number
}

interface ClearCheckinQuotaDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess?: () => void
}

export function ClearCheckinQuotaDialog({
  open,
  onOpenChange,
  onSuccess,
}: ClearCheckinQuotaDialogProps) {
  const { t } = useTranslation()
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [userScope, setUserScope] = useState<'all' | 'specific'>('all')
  const [userIdsText, setUserIdsText] = useState('')
  const [previewing, setPreviewing] = useState(false)
  const [clearing, setClearing] = useState(false)
  const [previewData, setPreviewData] = useState<PreviewData | null>(null)

  const reset = () => {
    setStartDate('')
    setEndDate('')
    setUserScope('all')
    setUserIdsText('')
    setPreviewData(null)
    setPreviewing(false)
    setClearing(false)
  }

  const handleClose = () => {
    reset()
    onOpenChange(false)
  }

  const buildRequestBody = (): {
    start_date: string
    end_date: string
    user_ids?: number[]
  } | null => {
    if (!startDate || !endDate) {
      toast.error(t('Please select a date range'))
      return null
    }
    const body: { start_date: string; end_date: string; user_ids?: number[] } =
      {
        start_date: startDate,
        end_date: endDate,
      }
    if (userScope === 'specific') {
      const ids = userIdsText
        .split(/[,，\s]+/)
        .map((s) => parseInt(s.trim(), 10))
        .filter((n) => !isNaN(n) && n > 0)
      if (ids.length === 0) {
        toast.error(t('Please enter valid user IDs'))
        return null
      }
      body.user_ids = ids
    }
    return body
  }

  const handlePreview = async () => {
    const body = buildRequestBody()
    if (!body) return
    setPreviewing(true)
    try {
      const res = await api.post('/api/user/checkin/clear/preview', body)
      const result = res.data as {
        success?: boolean
        message?: string
        data?: PreviewData
      }
      if (result.success && result.data) {
        setPreviewData(result.data)
      } else {
        toast.error(result.message || t('Request failed'))
      }
    } catch (_error) {
      toast.error(t('Request failed'))
    } finally {
      setPreviewing(false)
    }
  }

  const handleClear = async () => {
    const body = buildRequestBody()
    if (!body) return
    setClearing(true)
    try {
      const res = await api.post('/api/user/checkin/clear', body)
      const result = res.data as {
        success?: boolean
        message?: string
        data?: { message?: string }
      }
      if (result.success) {
        toast.success(result.data?.message || t('Cleared successfully'))
        handleClose()
        onSuccess?.()
      } else {
        toast.error(result.message || t('Request failed'))
      }
    } catch (_error) {
      toast.error(t('Request failed'))
    } finally {
      setClearing(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) handleClose()
        else onOpenChange(true)
      }}
    >
      <DialogContent className='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('Clear Check-in Quota')}</DialogTitle>
          <DialogDescription>
            {t(
              'Batch clear check-in quota by date range and user scope. Preview before confirming.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4'>
          <div className='space-y-2'>
            <Label>{t('Date Range')}</Label>
            <div className='flex items-center gap-2'>
              <Input
                type='date'
                value={startDate}
                onChange={(e) => {
                  setStartDate(e.target.value)
                  setPreviewData(null)
                }}
                className='h-9'
              />
              <span className='text-muted-foreground'>~</span>
              <Input
                type='date'
                value={endDate}
                onChange={(e) => {
                  setEndDate(e.target.value)
                  setPreviewData(null)
                }}
                className='h-9'
              />
            </div>
          </div>

          <div className='space-y-2'>
            <Label>{t('User Scope')}</Label>
            <RadioGroup
              value={userScope}
              onValueChange={(val: string) => {
                setUserScope(val as 'all' | 'specific')
                setPreviewData(null)
              }}
              className='flex gap-6'
            >
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='all' id='scope-all' />
                <Label htmlFor='scope-all' className='font-normal'>
                  {t('All Users')}
                </Label>
              </div>
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='specific' id='scope-specific' />
                <Label htmlFor='scope-specific' className='font-normal'>
                  {t('Specific Users')}
                </Label>
              </div>
            </RadioGroup>
          </div>

          {userScope === 'specific' && (
            <Textarea
              placeholder={t(
                'Enter user IDs separated by commas, e.g. 1, 2, 3'
              )}
              value={userIdsText}
              onChange={(e) => {
                setUserIdsText(e.target.value)
                setPreviewData(null)
              }}
              rows={2}
            />
          )}

          <Button
            variant='outline'
            onClick={handlePreview}
            disabled={previewing || !startDate || !endDate}
          >
            {previewing && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {t('Preview')}
          </Button>

          {previewData &&
            (previewData.users.length === 0 ? (
              <div className='rounded-md border border-blue-500/30 bg-blue-500/5 px-3 py-2 text-sm text-blue-600'>
                {t('No check-in records to clear in the selected range')}
              </div>
            ) : (
              <div className='space-y-3'>
                <div className='rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-sm text-amber-600'>
                  {t('Will affect {{count}} user(s), clearing {{quota}}', {
                    count: previewData.affected_users,
                    quota: formatQuota(previewData.total_actual_clear),
                  })}
                </div>
                <div className='max-h-64 overflow-auto rounded-md border'>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('User ID')}</TableHead>
                        <TableHead>{t('Username')}</TableHead>
                        <TableHead>{t('Check-in Quota')}</TableHead>
                        <TableHead>{t('Actual Clear')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {previewData.users.map((item) => (
                        <TableRow key={item.user_id}>
                          <TableCell>{item.user_id}</TableCell>
                          <TableCell>{item.username}</TableCell>
                          <TableCell>{formatQuota(item.checkin_quota)}</TableCell>
                          <TableCell className='text-destructive'>
                            -{formatQuota(item.actual_clear)}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </div>
            ))}
        </div>

        <DialogFooter className='grid grid-cols-2 gap-2 sm:flex'>
          <Button variant='outline' onClick={handleClose} disabled={clearing}>
            {t('Cancel')}
          </Button>
          <Button
            variant='destructive'
            onClick={handleClear}
            disabled={
              clearing ||
              !previewData ||
              previewData.users.length === 0
            }
          >
            {clearing && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {t('Confirm Clear')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
