/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/

import React, { useEffect, useMemo, useState, useRef } from 'react';
import { Button, Col, Form, Row, Spin, Typography } from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
  selectFilter,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

function parseAllowedModels(raw) {
  if (!raw || typeof raw !== 'string') return [];
  return raw
    .split(',')
    .map((m) => m.trim())
    .filter(Boolean);
}

export default function SettingsBonusQuota(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [enabledModels, setEnabledModels] = useState([]);
  const [allowedModels, setAllowedModels] = useState([]);
  const [inputs, setInputs] = useState({
    'bonus_quota_setting.validity_days': 0,
    'bonus_quota_setting.allowed_models': '',
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  const modelOptions = useMemo(() => {
    const set = new Set(enabledModels);
    for (const model of allowedModels) {
      set.add(model);
    }
    return Array.from(set)
      .sort((a, b) => a.localeCompare(b))
      .map((model) => ({ label: model, value: model }));
  }, [enabledModels, allowedModels]);

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function handleAllowedModelsChange(values) {
    const next = Array.isArray(values) ? values : [];
    setAllowedModels(next);
    setInputs((inputs) => ({
      ...inputs,
      'bonus_quota_setting.allowed_models': next.join(','),
    }));
  }

  async function loadEnabledModels() {
    setModelsLoading(true);
    try {
      const res = await API.get('/api/channel/models_enabled');
      const { success, message, data } = res.data;
      if (success) {
        setEnabledModels(Array.isArray(data) ? data : []);
      } else {
        showError(message || t('获取启用模型失败'));
      }
    } catch (error) {
      console.error(t('获取启用模型失败:'), error);
      showError(t('获取启用模型失败'));
    } finally {
      setModelsLoading(false);
    }
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) =>
      API.put('/api/option/', {
        key: item.key,
        value: String(inputs[item.key] ?? ''),
      }),
    );
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    loadEnabledModels();
  }, []);

  useEffect(() => {
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    setAllowedModels(parseAllowedModels(currentInputs['bonus_quota_setting.allowed_models']));
    refForm.current?.setValues(currentInputs);
  }, [props.options]);

  return (
    <Spin spinning={loading || modelsLoading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section text={t('奖励额度限制')}>
          <Typography.Text type='tertiary' style={{ marginBottom: 16, display: 'block' }}>
            {t('签到与邮箱绑定奖励共用有效期与可用模型限制')}
          </Typography.Text>
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8}>
              <Form.InputNumber
                field={'bonus_quota_setting.validity_days'}
                label={t('有效期（天）')}
                min={0}
                placeholder={t('0 表示永不过期')}
                onChange={handleFieldChange('bonus_quota_setting.validity_days')}
              />
            </Col>
            <Col xs={24} sm={24} md={16}>
              <Form.Select
                label={t('可用模型（留空表示不限制）')}
                placeholder={t('搜索或选择模型，也可输入自定义模型名')}
                multiple
                filter={selectFilter}
                allowCreate
                autoClearSearchValue={false}
                searchPosition='dropdown'
                showClear
                optionList={modelOptions}
                value={allowedModels}
                onChange={handleAllowedModelsChange}
                style={{ width: '100%' }}
                extraText={t(
                  '从已启用渠道模型中选择，支持搜索与手动输入自定义模型名',
                )}
              />
            </Col>
          </Row>
          <Row>
            <Button size='default' onClick={onSubmit}>
              {t('保存奖励额度设置')}
            </Button>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
