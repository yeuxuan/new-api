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
import { useMemo } from 'react'
import { Gift } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import dayjs from 'dayjs'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { BonusQuotaGrantSummary, UserProfile } from '../types'

interface BonusQuotaGrantsCardProps {
  profile: UserProfile | null
  loading: boolean
}

function formatExpiresAt(expiresAt: number, t: (key: string) => string) {
  if (expiresAt <= 0) {
    return t('Never expires')
  }
  return dayjs.unix(expiresAt).format('YYYY-MM-DD HH:mm')
}

function sourceLabel(source: string, t: (key: string) => string) {
  if (source === 'checkin') {
    return t('Check-in reward')
  }
  if (source === 'email_bind') {
    return t('Email bind reward')
  }
  return source
}

export function BonusQuotaGrantsCard({
  profile,
  loading,
}: BonusQuotaGrantsCardProps) {
  const { t } = useTranslation()

  const grants = useMemo(
    () =>
      (profile?.bonus_quota_grants ?? []).filter(
        (grant) => grant.amount_remaining > 0
      ),
    [profile?.bonus_quota_grants]
  )

  const bonusQuota = profile?.bonus_quota ?? 0
  const allowedModels = profile?.bonus_quota_allowed_models ?? []
  const validityDays = profile?.bonus_quota_validity_days ?? 0

  const policyHint = useMemo(() => {
    const parts: string[] = []
    if (validityDays > 0) {
      parts.push(t('Valid for {{days}} days', { days: validityDays }))
    }
    if (allowedModels.length > 0) {
      parts.push(t('Limited to selected models'))
    }
    return parts.join(' · ')
  }, [allowedModels.length, t, validityDays])

  if (!loading && bonusQuota <= 0 && grants.length === 0) {
    return null
  }

  return (
    <div className='bg-card overflow-hidden rounded-lg border'>
      <div className='flex items-start gap-3 border-b p-4 sm:p-5'>
        <div className='bg-primary/10 text-primary flex h-10 w-10 shrink-0 items-center justify-center rounded-xl'>
          <Gift className='h-4 w-4' strokeWidth={2} />
        </div>
        <div className='min-w-0 flex-1'>
          <h3 className='text-base font-semibold tracking-tight sm:text-lg'>
            {t('Bonus quota grants')}
          </h3>
          <p className='text-muted-foreground mt-1 text-xs sm:text-sm'>
            {loading
              ? t('Loading...')
              : t('Available bonus quota: {{amount}}', {
                  amount: formatQuotaWithCurrency(bonusQuota),
                })}
            {policyHint ? ` · ${policyHint}` : ''}
          </p>
        </div>
      </div>

      <div className='p-4 sm:p-5'>
        {loading ? (
          <div className='space-y-2'>
            <Skeleton className='h-8 w-full' />
            <Skeleton className='h-8 w-full' />
          </div>
        ) : grants.length === 0 ? (
          <p className='text-muted-foreground text-sm'>
            {t('No active bonus quota grants')}
          </p>
        ) : (
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Source')}</TableHead>
                  <TableHead>{t('Remaining')}</TableHead>
                  <TableHead>{t('Expires')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {grants.map((grant: BonusQuotaGrantSummary) => (
                  <TableRow key={grant.id}>
                    <TableCell>{sourceLabel(grant.source, t)}</TableCell>
                    <TableCell>
                      {formatQuotaWithCurrency(grant.amount_remaining)}
                    </TableCell>
                    <TableCell>
                      {formatExpiresAt(grant.expires_at, t)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </div>
    </div>
  )
}
