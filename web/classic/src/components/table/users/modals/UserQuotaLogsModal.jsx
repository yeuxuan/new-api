import React, { useEffect, useMemo, useState, useCallback } from 'react';
import {
  Empty,
  Select,
  SideSheet,
  Space,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { API, showError } from '../../../../helpers';
import { renderQuota } from '../../../../helpers';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';
import CardTable from '../../../common/ui/CardTable';

const { Text } = Typography;

const LOG_TYPE_OPTIONS = (t) => [
  { label: t('全部'), value: 0 },
  { label: t('充值'), value: 1 },
  { label: t('消费'), value: 2 },
  { label: t('管理'), value: 3 },
  { label: t('系统'), value: 4 },
  { label: t('错误'), value: 5 },
  { label: t('退款'), value: 6 },
];

function renderLogType(type, t) {
  const map = {
    1: { color: 'cyan', text: t('充值') },
    2: { color: 'lime', text: t('消费') },
    3: { color: 'orange', text: t('管理') },
    4: { color: 'purple', text: t('系统') },
    5: { color: 'red', text: t('错误') },
    6: { color: 'teal', text: t('退款') },
  };
  const info = map[type] || { color: 'grey', text: t('未知') };
  return (
    <Tag color={info.color} shape='circle' size='small'>
      {info.text}
    </Tag>
  );
}

function formatTimestamp(ts) {
  if (!ts) return '-';
  return new Date(ts * 1000).toLocaleString();
}

const UserQuotaLogsModal = ({ visible, onCancel, user, t }) => {
  const isMobile = useIsMobile();
  const [loading, setLoading] = useState(false);
  const [logs, setLogs] = useState([]);
  const [total, setTotal] = useState(0);
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [logType, setLogType] = useState(0);

  const loadLogs = useCallback(
    async (page, size, type) => {
      if (!user?.username) return;
      setLoading(true);
      try {
        const params = new URLSearchParams({
          username: user.username,
          p: page,
          page_size: size,
        });
        if (type > 0) {
          params.set('type', type);
        }
        const res = await API.get(`/api/log/?${params.toString()}`);
        const { success, data, message } = res.data;
        if (success) {
          setLogs(data.items || []);
          setTotal(data.total || 0);
        } else {
          showError(message);
        }
      } catch {
        showError(t('请求失败'));
      } finally {
        setLoading(false);
      }
    },
    [user?.username, t],
  );

  useEffect(() => {
    if (!visible) return;
    setCurrentPage(1);
    setLogType(0);
    loadLogs(1, pageSize, 0);
  }, [visible, user?.username]);

  const handlePageChange = (page) => {
    setCurrentPage(page);
    loadLogs(page, pageSize, logType);
  };

  const handlePageSizeChange = (size) => {
    setPageSize(size);
    setCurrentPage(1);
    loadLogs(1, size, logType);
  };

  const handleTypeChange = (type) => {
    setLogType(type);
    setCurrentPage(1);
    loadLogs(1, pageSize, type);
  };

  const columns = useMemo(
    () => [
      {
        title: t('时间'),
        dataIndex: 'created_at',
        width: 170,
        render: (text) => (
          <Text size='small'>{formatTimestamp(text)}</Text>
        ),
      },
      {
        title: t('类型'),
        dataIndex: 'type',
        width: 80,
        render: (text) => renderLogType(text, t),
      },
      {
        title: t('额度变动'),
        dataIndex: 'quota',
        width: 120,
        render: (text, record) => {
          const quota = parseInt(text) || 0;
          if (quota === 0) {
            const match = record.content?.match(/[\$＄]\s*([\d.]+)/);
            if (match) {
              const from = record.content?.match(/从\s*[\$＄]\s*([\d.]+)/);
              const to = record.content?.match(/修改为\s*[\$＄]\s*([\d.]+)/);
              const isDeduction = from && to
                ? parseFloat(to[1]) < parseFloat(from[1])
                : record.type === 2;
              return (
                <Text type={isDeduction ? 'danger' : 'success'}>
                  {isDeduction ? '-' : '+'}${match[1]}
                </Text>
              );
            }
            return <Text type='tertiary'>-</Text>;
          }
          const displayQuota = record.type === 2 ? -Math.abs(quota) : quota;
          return (
            <Text type={displayQuota < 0 ? 'danger' : 'success'}>
              {displayQuota < 0 ? '-' : '+'}
              {renderQuota(Math.abs(quota), 6)}
            </Text>
          );
        },
      },
      {
        title: t('模型'),
        dataIndex: 'model_name',
        width: 150,
        render: (text) =>
          text ? (
            <Tag color='blue' shape='circle' size='small'>
              {text}
            </Tag>
          ) : null,
      },
      {
        title: t('令牌'),
        dataIndex: 'token_name',
        width: 120,
        render: (text) =>
          text ? (
            <Tag color='grey' shape='circle' size='small'>
              {text}
            </Tag>
          ) : null,
      },
      {
        title: t('详情'),
        dataIndex: 'content',
        render: (text) => (
          <Typography.Paragraph
            ellipsis={{
              rows: 2,
              showTooltip: {
                type: 'popover',
                opts: { style: { width: 300 } },
              },
            }}
            style={{ maxWidth: 250, marginBottom: 0 }}
          >
            {text}
          </Typography.Paragraph>
        ),
      },
    ],
    [t],
  );

  return (
    <SideSheet
      visible={visible}
      placement='right'
      width={isMobile ? '100%' : 960}
      bodyStyle={{ padding: 0 }}
      onCancel={onCancel}
      title={
        <Space>
          <Tag color='blue' shape='circle'>
            {t('额度')}
          </Tag>
          <Typography.Title heading={4} className='m-0'>
            {t('额度变动明细')}
          </Typography.Title>
          <Text type='tertiary' className='ml-2'>
            {user?.username || '-'} (ID: {user?.id || '-'})
          </Text>
        </Space>
      }
    >
      <div className='p-4'>
        <div className='flex items-center gap-2 mb-4'>
          <Text>{t('类型')}:</Text>
          <Select
            value={logType}
            optionList={LOG_TYPE_OPTIONS(t)}
            onChange={handleTypeChange}
            style={{ width: 120 }}
            size='small'
          />
        </div>

        <CardTable
          columns={columns}
          dataSource={logs}
          rowKey='id'
          loading={loading}
          scroll={{ x: 'max-content' }}
          hidePagination={false}
          pagination={{
            currentPage,
            pageSize,
            total,
            pageSizeOpts: [10, 20, 50, 100],
            showSizeChanger: true,
            onPageChange: handlePageChange,
            onPageSizeChange: handlePageSizeChange,
          }}
          empty={
            <Empty
              image={
                <IllustrationNoResult style={{ width: 150, height: 150 }} />
              }
              darkModeImage={
                <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
              }
              description={t('暂无记录')}
              style={{ padding: 30 }}
            />
          }
          size='middle'
        />
      </div>
    </SideSheet>
  );
};

export default UserQuotaLogsModal;
