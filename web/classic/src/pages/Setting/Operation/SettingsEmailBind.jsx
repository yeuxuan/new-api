/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/

import React, { useEffect, useState, useRef } from 'react';
import { Button, Col, Form, Row, Spin, Typography } from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function SettingsEmailBind(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    'email_bind_setting.enabled': false,
    'email_bind_setting.quota': 0,
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) =>
      API.put('/api/option/', {
        key: item.key,
        value: String(inputs[item.key]),
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
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section text={t('邮箱绑定奖励')}>
          <Typography.Text type='tertiary' style={{ marginBottom: 16, display: 'block' }}>
            {t('用户首次绑定邮箱时可获得奖励额度')}
          </Typography.Text>
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8}>
              <Form.Switch
                field={'email_bind_setting.enabled'}
                label={t('启用邮箱绑定奖励')}
                checkedText='｜'
                uncheckedText='〇'
                onChange={handleFieldChange('email_bind_setting.enabled')}
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.InputNumber
                field={'email_bind_setting.quota'}
                label={t('绑定赠送额度')}
                min={0}
                disabled={!inputs['email_bind_setting.enabled']}
                onChange={handleFieldChange('email_bind_setting.quota')}
              />
            </Col>
          </Row>
          <Row>
            <Button size='default' onClick={onSubmit}>
              {t('保存邮箱绑定设置')}
            </Button>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
