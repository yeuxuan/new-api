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

import React from 'react';
import { Typography, Tag, Space } from '@douyinfe/semi-ui';
import { IconUserAdd } from '@douyinfe/semi-icons';
import CompactModeToggle from '../../common/ui/CompactModeToggle';
import { renderQuota } from '../../../helpers';

const { Text } = Typography;

const UsersDescription = ({ compactMode, setCompactMode, quotaSummary, t }) => {
  return (
    <div className='flex flex-col md:flex-row justify-between items-start md:items-center gap-2 w-full'>
      <div className='flex items-center gap-3 flex-wrap'>
        <div className='flex items-center text-blue-500'>
          <IconUserAdd className='mr-2' />
          <Text>{t('用户管理')}</Text>
        </div>
        {quotaSummary && (
          <Space spacing={4}>
            <Tag color='blue' shape='circle' size='small'>
              {t('用户总数')}: {quotaSummary.total_user_count}
            </Tag>
            <Tag color='green' shape='circle' size='small'>
              {t('剩余总额度')}: {renderQuota(quotaSummary.total_quota)}
            </Tag>
            <Tag color='orange' shape='circle' size='small'>
              {t('已用总额度')}: {renderQuota(quotaSummary.total_used_quota)}
            </Tag>
          </Space>
        )}
      </div>
      <CompactModeToggle
        compactMode={compactMode}
        setCompactMode={setCompactMode}
        t={t}
      />
    </div>
  );
};

export default UsersDescription;
