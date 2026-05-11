import React, { useState } from 'react';
import {
  Modal,
  DatePicker,
  RadioGroup,
  Radio,
  TextArea,
  Table,
  Typography,
  Banner,
  Space,
  Button,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../../../helpers';
import { renderQuota } from '../../../../helpers/render';

const ClearCheckinQuotaModal = ({ visible, onCancel, refresh }) => {
  const { t } = useTranslation();
  const [dateRange, setDateRange] = useState([]);
  const [userScope, setUserScope] = useState('all');
  const [userIdsText, setUserIdsText] = useState('');
  const [previewing, setPreviewing] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [previewData, setPreviewData] = useState(null);

  const reset = () => {
    setDateRange([]);
    setUserScope('all');
    setUserIdsText('');
    setPreviewData(null);
    setPreviewing(false);
    setClearing(false);
  };

  const handleClose = () => {
    reset();
    onCancel();
  };

  const buildRequestBody = () => {
    if (!dateRange || dateRange.length !== 2) {
      showError(t('请选择日期范围'));
      return null;
    }
    const formatDate = (d) => {
      const date = new Date(d);
      const y = date.getFullYear();
      const m = String(date.getMonth() + 1).padStart(2, '0');
      const day = String(date.getDate()).padStart(2, '0');
      return `${y}-${m}-${day}`;
    };
    const body = {
      start_date: formatDate(dateRange[0]),
      end_date: formatDate(dateRange[1]),
    };
    if (userScope === 'specific') {
      const ids = userIdsText
        .split(/[,，\s]+/)
        .map((s) => parseInt(s.trim(), 10))
        .filter((n) => !isNaN(n) && n > 0);
      if (ids.length === 0) {
        showError(t('请输入有效的用户 ID'));
        return null;
      }
      body.user_ids = ids;
    }
    return body;
  };

  const handlePreview = async () => {
    const body = buildRequestBody();
    if (!body) return;
    setPreviewing(true);
    try {
      const res = await API.post('/api/user/checkin/clear/preview', body);
      const { success, message, data } = res.data;
      if (success) {
        setPreviewData(data);
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    } finally {
      setPreviewing(false);
    }
  };

  const handleClear = async () => {
    const body = buildRequestBody();
    if (!body) return;
    setClearing(true);
    try {
      const res = await API.post('/api/user/checkin/clear', body);
      const { success, message, data } = res.data;
      if (success) {
        showSuccess(data.message || t('清除成功'));
        handleClose();
        refresh();
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    } finally {
      setClearing(false);
    }
  };

  const previewColumns = [
    { title: t('用户 ID'), dataIndex: 'user_id', width: 80 },
    { title: t('用户名'), dataIndex: 'username', width: 120 },
    {
      title: t('签到额度'),
      dataIndex: 'checkin_quota',
      width: 120,
      render: (val) => renderQuota(val),
    },
    {
      title: t('当前额度'),
      dataIndex: 'current_quota',
      width: 120,
      render: (val) => renderQuota(val),
    },
    {
      title: t('实际清除'),
      dataIndex: 'actual_clear',
      width: 120,
      render: (val) => (
        <Typography.Text type='danger'>-{renderQuota(val)}</Typography.Text>
      ),
    },
  ];

  return (
    <Modal
      title={t('清除签到额度')}
      visible={visible}
      onCancel={handleClose}
      footer={null}
      width={700}
      closeOnEsc
    >
      <Space vertical align='start' spacing='medium' style={{ width: '100%' }}>
        <div>
          <Typography.Text strong>{t('日期范围')}</Typography.Text>
          <DatePicker
            type='dateRange'
            style={{ width: '100%', marginTop: 8 }}
            value={dateRange}
            onChange={(val) => {
              setDateRange(val);
              setPreviewData(null);
            }}
            placeholder={[t('开始日期'), t('结束日期')]}
          />
        </div>

        <div>
          <Typography.Text strong>{t('用户范围')}</Typography.Text>
          <RadioGroup
            value={userScope}
            onChange={(e) => {
              setUserScope(e.target.value);
              setPreviewData(null);
            }}
            style={{ marginTop: 8 }}
          >
            <Radio value='all'>{t('全部用户')}</Radio>
            <Radio value='specific'>{t('指定用户')}</Radio>
          </RadioGroup>
        </div>

        {userScope === 'specific' && (
          <TextArea
            placeholder={t('请输入用户 ID，多个用逗号分隔，例如：1, 2, 3')}
            value={userIdsText}
            onChange={(val) => {
              setUserIdsText(val);
              setPreviewData(null);
            }}
            autosize={{ minRows: 2, maxRows: 4 }}
          />
        )}

        <Button
          onClick={handlePreview}
          loading={previewing}
          disabled={!dateRange || dateRange.length !== 2}
        >
          {t('预览')}
        </Button>

        {previewData && (
          <>
            {previewData.users.length === 0 ? (
              <Banner
                type='info'
                description={t('所选范围内没有可清除的签到记录')}
              />
            ) : (
              <>
                <Banner
                  type='warning'
                  description={t('将影响 {{count}} 个用户，实际清除额度 {{quota}}', {
                    count: previewData.affected_users,
                    quota: renderQuota(previewData.total_actual_clear),
                  })}
                />
                <Table
                  columns={previewColumns}
                  dataSource={previewData.users}
                  rowKey='user_id'
                  pagination={previewData.users.length > 10 ? { pageSize: 10 } : false}
                  size='small'
                  style={{ width: '100%' }}
                />
                <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
                  <Button onClick={handleClose}>{t('取消')}</Button>
                  <Button
                    type='danger'
                    theme='solid'
                    loading={clearing}
                    onClick={handleClear}
                  >
                    {t('确认清除')}
                  </Button>
                </div>
              </>
            )}
          </>
        )}
      </Space>
    </Modal>
  );
};

export default ClearCheckinQuotaModal;
