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

import React, { useMemo } from 'react';
import { Card, Table, Typography } from '@douyinfe/semi-ui';
import { Gift } from 'lucide-react';
import { renderQuota } from '../../../../helpers';

function formatExpiresAt(expiresAt, t) {
  if (!expiresAt || expiresAt <= 0) {
    return t('Never expires');
  }
  return new Date(expiresAt * 1000).toLocaleString();
}

function sourceLabel(source, t) {
  if (source === 'checkin') {
    return t('Check-in reward');
  }
  if (source === 'email_bind') {
    return t('Email bind reward');
  }
  return source;
}

const BonusQuotaGrants = ({ t, userState, status }) => {
  const user = userState?.user;
  const grants = useMemo(
    () => (user?.bonus_quota_grants ?? []).filter((g) => g.amount_remaining > 0),
    [user?.bonus_quota_grants],
  );
  const bonusQuota = user?.bonus_quota ?? 0;
  const validityDays =
    user?.bonus_quota_validity_days ?? status?.bonus_quota_validity_days ?? 0;
  const allowedModels =
    user?.bonus_quota_allowed_models ?? status?.bonus_quota_allowed_models ?? [];

  const featuresEnabled =
    status?.checkin_enabled === true || status?.email_bind_reward_enabled === true;
  const hasBonus = bonusQuota > 0 || grants.length > 0;
  if (!hasBonus && !featuresEnabled) {
    return null;
  }

  const policyParts = [];
  if (validityDays > 0) {
    policyParts.push(t('Valid for {{days}} days', { days: validityDays }));
  }
  if (allowedModels.length > 0) {
    policyParts.push(t('Limited to selected models'));
  }

  const columns = [
    {
      title: t('Source'),
      dataIndex: 'source',
      render: (source) => sourceLabel(source, t),
    },
    {
      title: t('Remaining'),
      dataIndex: 'amount_remaining',
      render: (amount) => renderQuota(amount),
    },
    {
      title: t('Expires'),
      dataIndex: 'expires_at',
      render: (expiresAt) => formatExpiresAt(expiresAt, t),
    },
  ];

  return (
    <Card className='!rounded-2xl mt-4 md:mt-6'>
      <div className='flex items-start gap-3 mb-4'>
        <div className='w-10 h-10 rounded-xl bg-green-500/10 text-green-600 flex items-center justify-center'>
          <Gift size={18} />
        </div>
        <div>
          <Typography.Title heading={5} className='!mb-1'>
            {t('Limited-time bonus quota')}
          </Typography.Title>
          <Typography.Text type='tertiary' className='text-sm'>
            {t('Available bonus quota: {{amount}}', {
              amount: renderQuota(bonusQuota),
            })}
            {policyParts.length > 0 ? ` · ${policyParts.join(' · ')}` : ''}
          </Typography.Text>
        </div>
      </div>
      {grants.length === 0 ? (
        <Typography.Text type='tertiary'>
          {hasBonus
            ? t('No active bonus quota grants')
            : t(
                'Check in or bind your email to earn limited-time bonus quota. Expiring grants appear here.',
              )}
        </Typography.Text>
      ) : (
        <Table columns={columns} dataSource={grants} rowKey='id' pagination={false} />
      )}
    </Card>
  );
};

export default BonusQuotaGrants;
