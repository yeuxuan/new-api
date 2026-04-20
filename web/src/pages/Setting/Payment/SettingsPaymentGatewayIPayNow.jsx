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

import React, { useEffect, useState, useRef } from 'react';
import {
  Banner,
  Button,
  Form,
  Row,
  Col,
  Typography,
  Spin,
} from '@douyinfe/semi-ui';
const { Text } = Typography;
import {
  API,
  removeTrailingSlash,
  showError,
  showSuccess,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function SettingsPaymentGatewayIPayNow(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    IPayNowAppId: '',
    IPayNowAppKey: '',
    IPayNowMinTopUp: 1,
  });
  const [originInputs, setOriginInputs] = useState({});
  const formApiRef = useRef(null);

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        IPayNowAppId: props.options.IPayNowAppId || '',
        IPayNowAppKey: props.options.IPayNowAppKey || '',
        IPayNowMinTopUp:
          props.options.IPayNowMinTopUp !== undefined
            ? parseFloat(props.options.IPayNowMinTopUp)
            : 1,
      };
      setInputs(currentInputs);
      setOriginInputs({ ...currentInputs });
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const handleFormChange = (values) => {
    setInputs(values);
  };

  const submitSetting = async () => {
    if (props.options.ServerAddress === '') {
      showError(t('请先填写服务器地址'));
      return;
    }

    setLoading(true);
    try {
      const options = [];
      if (inputs.IPayNowAppId !== '') {
        options.push({ key: 'IPayNowAppId', value: inputs.IPayNowAppId });
      }
      if (inputs.IPayNowAppKey && inputs.IPayNowAppKey !== '') {
        options.push({ key: 'IPayNowAppKey', value: inputs.IPayNowAppKey });
      }
      if (
        inputs.IPayNowMinTopUp !== undefined &&
        inputs.IPayNowMinTopUp !== null &&
        inputs.IPayNowMinTopUp !== ''
      ) {
        options.push({
          key: 'IPayNowMinTopUp',
          value: inputs.IPayNowMinTopUp.toString(),
        });
      }

      const requestQueue = options.map((opt) =>
        API.put('/api/option/', {
          key: opt.key,
          value: opt.value,
        }),
      );

      const results = await Promise.all(requestQueue);

      const errorResults = results.filter((res) => !res.data.success);
      if (errorResults.length > 0) {
        errorResults.forEach((res) => showError(res.data.message));
      } else {
        showSuccess(t('更新成功'));
        setOriginInputs({ ...inputs });
        props.refresh?.();
      }
    } catch (error) {
      showError(t('更新失败'));
    }
    setLoading(false);
  };

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={handleFormChange}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={t('iPayNow 聚合动态码设置')}>
          <Text>
            {t('现在支付（iPayNow）聚合动态码：用户扫描 QR 码使用微信/支付宝完成支付。商户后台请在')}
            <a
              href='https://mch.ipaynow.cn/'
              target='_blank'
              rel='noreferrer'
            >
              {t(' iPayNow 商户服务 ')}
            </a>
            {t('申请应用获取。')}
          </Text>
          <Banner
            type='info'
            description={`${t('异步通知地址填：')}${
              props.options.ServerAddress
                ? removeTrailingSlash(props.options.ServerAddress)
                : t('网站地址')
            }/api/ipaynow/notify`}
          />
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='IPayNowAppId'
                label={t('应用编号')}
                placeholder={t('iPayNow 后台创建的应用编号（appId）')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='IPayNowAppKey'
                label={t('应用密钥')}
                placeholder={t('iPayNow 应用密钥，敏感信息不会回显')}
                type='password'
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='IPayNowMinTopUp'
                label={t('最低充值数量')}
                placeholder={t('例如：1，表示最低充值 1 个单位')}
              />
            </Col>
          </Row>
          <Button onClick={submitSetting}>{t('更新 iPayNow 设置')}</Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
