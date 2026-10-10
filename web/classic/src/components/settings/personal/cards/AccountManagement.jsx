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
import {
  Button,
  Card,
  Input,
  Space,
  Typography,
  Avatar,
  Tabs,
  TabPane,
  Popover,
  Modal,
  Checkbox,
  Radio,
  RadioGroup,
} from '@douyinfe/semi-ui';
import {
  IconMail,
  IconShield,
  IconGithubLogo,
  IconKey,
  IconLock,
  IconDelete,
} from '@douyinfe/semi-icons';
import { SiTelegram, SiWechat, SiLinux, SiDiscord } from 'react-icons/si';
import { UserPlus, ShieldCheck } from 'lucide-react';
import {
  API,
  showError,
  showSuccess,
  showWarning,
  getOAuthProviderIcon,
  renderQuota,
} from '../../../../helpers';
import {
  prepareOAuthBinding,
  bindOAuthAccount,
  unbindOAuthAccount,
} from '../../../../helpers/oauthVerification';
import { useSecureVerification } from '../../../../hooks/common/useSecureVerification';
import SecureVerificationModal from '../../../common/modals/SecureVerificationModal';
import TwoFASetting from '../components/TwoFASetting';

const AccountManagement = ({
  t,
  userState,
  status,
  systemToken,
  setShowEmailBindModal,
  setShowWeChatBindModal,
  generateAccessToken,
  securityPending,
  handleSystemTokenClick,
  setShowChangePasswordModal,
  setShowAccountDeleteModal,
  passkeyStatus,
  passkeySupported,
  passkeyRegisterLoading,
  passkeyDeleteLoading,
  onPasskeyRegister,
  onPasskeyDelete,
  onUpdate,
}) => {
  const renderAccountInfo = (accountId, label) => {
    if (!accountId || accountId === '') {
      return <span className='text-gray-500'>{t('未绑定')}</span>;
    }

    const popContent = (
      <div className='text-xs p-2'>
        <Typography.Paragraph copyable={{ content: accountId }}>
          {accountId}
        </Typography.Paragraph>
        {label ? (
          <div className='mt-1 text-[11px] text-gray-500'>{label}</div>
        ) : null}
      </div>
    );

    return (
      <Popover content={popContent} position='top' trigger='hover'>
        <span className='block max-w-full truncate text-gray-600 hover:text-blue-600 cursor-pointer'>
          {accountId}
        </span>
      </Popover>
    );
  };
  const isBound = (accountId) => Boolean(accountId);
  const [customOAuthBindings, setCustomOAuthBindings] = React.useState([]);
  const [bindingPending, setBindingPending] = React.useState(false);
  const [preparedBinding, setPreparedBinding] = React.useState(null);
  const [unbindTarget, setUnbindTarget] = React.useState(null);
  const bindingRequest = React.useRef(null);
  const security = useSecureVerification({ onError: () => cancelBinding() });
  const [tokenFormOpen, setTokenFormOpen] = React.useState(false);
  const [tokenCatalog, setTokenCatalog] = React.useState([]);
  const [tokenCatalogLoading, setTokenCatalogLoading] = React.useState(false);
  const [tokenName, setTokenName] = React.useState('');
  const [tokenScopes, setTokenScopes] = React.useState([]);
  const [tokenExpiryDays, setTokenExpiryDays] = React.useState(30);
  const [defaultTokenExpiryDays, setDefaultTokenExpiryDays] =
    React.useState(30);
  const tokenCatalogRequest = React.useRef(null);
  const tokenScopeOptions = tokenCatalog.flatMap((group) =>
    (Array.isArray(group.resources) ? group.resources : []).flatMap(
      (resource) =>
        (Array.isArray(resource.actions) ? resource.actions : []).map(
          (action) => ({
            value: `${resource.resource}:${action.action}`,
            label: `${t(resource.label_key)} / ${t(action.label_key)}`,
          }),
        ),
    ),
  );
  const tokenExpiryOptions = [
    ...new Set([30, 90, 180, 365, defaultTokenExpiryDays]),
  ];

  const openTokenForm = async () => {
    if (securityPending || tokenCatalogRequest.current) return;
    const controller = new AbortController();
    tokenCatalogRequest.current = controller;
    setTokenCatalogLoading(true);
    try {
      const res = await API.get('/api/user/access_tokens/catalog', {
        skipErrorHandler: true,
        signal: controller.signal,
      });
      controller.signal.throwIfAborted();
      if (!res.data.success || !Array.isArray(res.data.data?.groups)) {
        throw new Error(res.data.message || t('Failed to load access tokens'));
      }
      const defaultDays = res.data.data.default_expiry_days;
      const days =
        Number.isSafeInteger(defaultDays) && defaultDays > 0 ? defaultDays : 30;
      setTokenCatalog(res.data.data.groups);
      setDefaultTokenExpiryDays(days);
      setTokenExpiryDays(days);
      setTokenScopes([]);
      setTokenName('');
      setTokenFormOpen(true);
    } catch (error) {
      if (!controller.signal.aborted) showError(error);
    } finally {
      if (tokenCatalogRequest.current === controller) {
        tokenCatalogRequest.current = null;
        setTokenCatalogLoading(false);
      }
    }
  };

  const createSelectedToken = () => {
    if (securityPending || !tokenName.trim() || !tokenScopes.length) return;
    const allowed = new Set(tokenScopeOptions.map((option) => option.value));
    if (tokenScopes.some((scope) => !allowed.has(scope))) return;
    const input = {
      name: tokenName,
      scopes: [...tokenScopes],
      expires_at: Math.floor(Date.now() / 1000) + tokenExpiryDays * 86400,
    };
    setTokenFormOpen(false);
    generateAccessToken(input);
  };

  React.useEffect(() => {
    setTokenFormOpen(false);
    setTokenCatalog([]);
    setTokenScopes([]);
    setTokenName('');
    setTokenCatalogLoading(false);
    return () => {
      tokenCatalogRequest.current?.abort();
      tokenCatalogRequest.current = null;
    };
  }, [userState.user?.id, userState.user?.session?.sid]);

  const cancelBinding = () => {
    bindingRequest.current?.abort();
    bindingRequest.current = null;
    setPreparedBinding(null);
    setUnbindTarget(null);
    setBindingPending(false);
    security.cancelVerification();
  };

  React.useEffect(() => {
    cancelBinding();
    return () => bindingRequest.current?.abort();
  }, [userState.user?.id, userState.user?.session?.sid]);

  const startOAuthBinding = async (provider) => {
    if (bindingRequest.current) return;
    const controller = new AbortController();
    bindingRequest.current = controller;
    setBindingPending(true);
    const result = await security.startVerification(
      async (proof, signal) => {
        const authorization = await prepareOAuthBinding(
          provider,
          proof,
          signal,
        );
        controller.signal.throwIfAborted();
        // Open the popup on the next user click, not after asynchronous verification.
        setPreparedBinding({ provider, ...authorization });
      },
      { scope: 'account.binding.bind', context: { provider } },
    );
    if (result === false) cancelBinding();
  };

  const completeOAuthBinding = async () => {
    if (!preparedBinding || !bindingRequest.current) return;
    const prepared = preparedBinding;
    const controller = bindingRequest.current;
    setPreparedBinding(null);
    try {
      const result = await bindOAuthAccount(
        prepared.provider,
        undefined,
        controller.signal,
        prepared,
      );
      controller.signal.throwIfAborted();
      showSuccess(t('绑定成功！'));
      if (result?.notification_warning) {
        showWarning(
          t(
            'The change succeeded, but the notification email could not be sent.',
          ),
        );
      }
      await onUpdate?.();
      await loadCustomOAuthBindings();
    } catch (error) {
      if (!controller.signal.aborted) {
        showError(
          t(error.response?.data?.message || error.message || '操作失败'),
        );
      }
    } finally {
      if (bindingRequest.current === controller) cancelBinding();
    }
  };

  // Fetch custom OAuth bindings
  const loadCustomOAuthBindings = async () => {
    try {
      const res = await API.get('/api/user/oauth/bindings');
      if (res.data.success) {
        setCustomOAuthBindings(res.data.data || []);
      } else {
        showError(res.data.message || t('获取绑定信息失败'));
      }
    } catch (error) {
      showError(
        error.response?.data?.message || error.message || t('获取绑定信息失败'),
      );
    }
  };

  const handleUnbindCustomOAuth = (providerId, providerName) => {
    if (!bindingRequest.current) setUnbindTarget({ providerId, providerName });
  };

  const confirmOAuthUnbinding = async () => {
    if (!unbindTarget || bindingRequest.current) return;
    const { providerId } = unbindTarget;
    setUnbindTarget(null);
    const controller = new AbortController();
    bindingRequest.current = controller;
    setBindingPending(true);
    const result = await security.startVerification(
      async (proof, signal) => {
        const result = await unbindOAuthAccount(providerId, proof, signal);
        controller.signal.throwIfAborted();
        showSuccess(t('解绑成功'));
        if (result?.notification_warning) {
          showWarning(
            t(
              'The change succeeded, but the notification email could not be sent.',
            ),
          );
        }
        await onUpdate?.();
        await loadCustomOAuthBindings();
        bindingRequest.current = null;
        setBindingPending(false);
      },
      {
        scope: 'account.binding.unbind',
        context: { provider_id: Number(providerId) },
      },
    );
    if (result === false) cancelBinding();
  };

  // Handle bind custom OAuth
  const handleBindCustomOAuth = (provider) => {
    return startOAuthBinding(provider.slug);
  };

  // Check if custom OAuth provider is bound
  const isCustomOAuthBound = (providerId) => {
    const normalizedId = Number(providerId);
    return customOAuthBindings.some(
      (b) => Number(b.provider_id) === normalizedId,
    );
  };

  // Get binding info for a provider
  const getCustomOAuthBinding = (providerId) => {
    const normalizedId = Number(providerId);
    return customOAuthBindings.find(
      (b) => Number(b.provider_id) === normalizedId,
    );
  };

  React.useEffect(() => {
    loadCustomOAuthBindings();
  }, []);

  const passkeyEnabled = passkeyStatus?.enabled;
  const lastUsedLabel = passkeyStatus?.last_used_at
    ? new Date(passkeyStatus.last_used_at).toLocaleString()
    : t('尚未使用');

  return (
    <Card className='!rounded-2xl'>
      {/* 卡片头部 */}
      <div className='flex items-center mb-4'>
        <Avatar size='small' color='teal' className='mr-3 shadow-md'>
          <UserPlus size={16} />
        </Avatar>
        <div>
          <Typography.Text className='text-lg font-medium'>
            {t('账户管理')}
          </Typography.Text>
          <div className='text-xs text-gray-600'>
            {t('账户绑定、安全设置和身份验证')}
          </div>
        </div>
      </div>

      <Tabs type='card' defaultActiveKey='binding'>
        {/* 账户绑定 Tab */}
        <TabPane
          tab={
            <div className='flex items-center'>
              <UserPlus size={16} className='mr-2' />
              {t('账户绑定')}
            </div>
          }
          itemKey='binding'
        >
          <div className='py-4'>
            <div className='grid grid-cols-1 lg:grid-cols-2 gap-4'>
              {/* 邮箱绑定 */}
              <Card className='!rounded-xl'>
                <div className='flex items-center justify-between gap-3'>
                  <div className='flex items-center flex-1 min-w-0'>
                    <div className='w-10 h-10 rounded-full bg-slate-100 dark:bg-slate-700 flex items-center justify-center mr-3 flex-shrink-0'>
                      <IconMail
                        size='default'
                        className='text-slate-600 dark:text-slate-300'
                      />
                    </div>
                    <div className='flex-1 min-w-0'>
                      <div className='font-medium text-gray-900'>
                        {t('邮箱')}
                      </div>
                      <div className='text-sm text-gray-500 truncate'>
                        {renderAccountInfo(
                          userState.user?.email,
                          t('邮箱地址'),
                        )}
                      </div>
                      {!isBound(userState.user?.email) &&
                        status?.email_bind_reward_enabled === true &&
                        (status.email_bind_reward_quota ?? 0) > 0 && (
                          <div className='text-xs text-blue-600 dark:text-blue-400 mt-1'>
                            {t('Bind to receive {{amount}} bonus quota', {
                              amount: renderQuota(
                                status.email_bind_reward_quota,
                              ),
                            })}
                            {(status.bonus_quota_validity_days ?? 0) > 0
                              ? ` · ${t('Valid for {{days}} days', {
                                  days: status.bonus_quota_validity_days,
                                })}`
                              : ` · ${t('Never expires')}`}
                          </div>
                        )}
                    </div>
                  </div>
                  <div className='flex-shrink-0'>
                    <Button
                      type='primary'
                      theme='outline'
                      size='small'
                      onClick={() => setShowEmailBindModal(true)}
                    >
                      {isBound(userState.user?.email)
                        ? t('修改绑定')
                        : t('绑定')}
                    </Button>
                  </div>
                </div>
              </Card>

              {/* 微信绑定 */}
              <Card className='!rounded-xl'>
                <div className='flex items-center justify-between gap-3'>
                  <div className='flex items-center flex-1 min-w-0'>
                    <div className='w-10 h-10 rounded-full bg-slate-100 dark:bg-slate-700 flex items-center justify-center mr-3 flex-shrink-0'>
                      <SiWechat
                        size={20}
                        className='text-slate-600 dark:text-slate-300'
                      />
                    </div>
                    <div className='flex-1 min-w-0'>
                      <div className='font-medium text-gray-900'>
                        {t('微信')}
                      </div>
                      <div className='text-sm text-gray-500 truncate'>
                        {!status.wechat_login
                          ? t('未启用')
                          : isBound(userState.user?.wechat_id)
                            ? t('已绑定')
                            : t('未绑定')}
                      </div>
                    </div>
                  </div>
                  <div className='flex-shrink-0'>
                    <Button
                      type='primary'
                      theme='outline'
                      size='small'
                      disabled={!status.wechat_login}
                      onClick={() => setShowWeChatBindModal(true)}
                    >
                      {isBound(userState.user?.wechat_id)
                        ? t('修改绑定')
                        : status.wechat_login
                          ? t('绑定')
                          : t('未启用')}
                    </Button>
                  </div>
                </div>
              </Card>

              {/* GitHub绑定 */}
              <Card className='!rounded-xl'>
                <div className='flex items-center justify-between gap-3'>
                  <div className='flex items-center flex-1 min-w-0'>
                    <div className='w-10 h-10 rounded-full bg-slate-100 dark:bg-slate-700 flex items-center justify-center mr-3 flex-shrink-0'>
                      <IconGithubLogo
                        size='default'
                        className='text-slate-600 dark:text-slate-300'
                      />
                    </div>
                    <div className='flex-1 min-w-0'>
                      <div className='font-medium text-gray-900'>
                        {t('GitHub')}
                      </div>
                      <div className='text-sm text-gray-500 truncate'>
                        {renderAccountInfo(
                          userState.user?.github_id,
                          t('GitHub ID'),
                        )}
                      </div>
                    </div>
                  </div>
                  <div className='flex-shrink-0'>
                    <Button
                      type='primary'
                      theme='outline'
                      size='small'
                      onClick={() => startOAuthBinding('github')}
                      disabled={
                        bindingPending ||
                        isBound(userState.user?.github_id) ||
                        !status.github_oauth
                      }
                    >
                      {status.github_oauth ? t('绑定') : t('未启用')}
                    </Button>
                  </div>
                </div>
              </Card>

              {/* Discord绑定 */}
              <Card className='!rounded-xl'>
                <div className='flex items-center justify-between gap-3'>
                  <div className='flex items-center flex-1 min-w-0'>
                    <div className='w-10 h-10 rounded-full bg-slate-100 dark:bg-slate-700 flex items-center justify-center mr-3 flex-shrink-0'>
                      <SiDiscord
                        size={20}
                        className='text-slate-600 dark:text-slate-300'
                      />
                    </div>
                    <div className='flex-1 min-w-0'>
                      <div className='font-medium text-gray-900'>
                        {t('Discord')}
                      </div>
                      <div className='text-sm text-gray-500 truncate'>
                        {renderAccountInfo(
                          userState.user?.discord_id,
                          t('Discord ID'),
                        )}
                      </div>
                    </div>
                  </div>
                  <div className='flex-shrink-0'>
                    <Button
                      type='primary'
                      theme='outline'
                      size='small'
                      onClick={() => startOAuthBinding('discord')}
                      disabled={
                        bindingPending ||
                        isBound(userState.user?.discord_id) ||
                        !status.discord_oauth
                      }
                    >
                      {status.discord_oauth ? t('绑定') : t('未启用')}
                    </Button>
                  </div>
                </div>
              </Card>

              {/* OIDC绑定 */}
              <Card className='!rounded-xl'>
                <div className='flex items-center justify-between gap-3'>
                  <div className='flex items-center flex-1 min-w-0'>
                    <div className='w-10 h-10 rounded-full bg-slate-100 dark:bg-slate-700 flex items-center justify-center mr-3 flex-shrink-0'>
                      <IconShield
                        size='default'
                        className='text-slate-600 dark:text-slate-300'
                      />
                    </div>
                    <div className='flex-1 min-w-0'>
                      <div className='font-medium text-gray-900'>
                        {t('OIDC')}
                      </div>
                      <div className='text-sm text-gray-500 truncate'>
                        {renderAccountInfo(
                          userState.user?.oidc_id,
                          t('OIDC ID'),
                        )}
                      </div>
                    </div>
                  </div>
                  <div className='flex-shrink-0'>
                    <Button
                      type='primary'
                      theme='outline'
                      size='small'
                      onClick={() => startOAuthBinding('oidc')}
                      disabled={
                        bindingPending ||
                        isBound(userState.user?.oidc_id) ||
                        !status.oidc_enabled
                      }
                    >
                      {status.oidc_enabled ? t('绑定') : t('未启用')}
                    </Button>
                  </div>
                </div>
              </Card>

              {/* Telegram绑定 */}
              <Card className='!rounded-xl'>
                <div className='flex items-center justify-between gap-3'>
                  <div className='flex items-center flex-1 min-w-0'>
                    <div className='w-10 h-10 rounded-full bg-slate-100 dark:bg-slate-700 flex items-center justify-center mr-3 flex-shrink-0'>
                      <SiTelegram
                        size={20}
                        className='text-slate-600 dark:text-slate-300'
                      />
                    </div>
                    <div className='flex-1 min-w-0'>
                      <div className='font-medium text-gray-900'>
                        {t('Telegram')}
                      </div>
                      <div className='text-sm text-gray-500 truncate'>
                        {renderAccountInfo(
                          userState.user?.telegram_id,
                          t('Telegram ID'),
                        )}
                      </div>
                    </div>
                  </div>
                  <div className='flex-shrink-0'>
                    {status.telegram_oauth ? (
                      isBound(userState.user?.telegram_id) ? (
                        <Button
                          disabled
                          size='small'
                          type='primary'
                          theme='outline'
                        >
                          {t('已绑定')}
                        </Button>
                      ) : (
                        <Button
                          type='primary'
                          theme='outline'
                          size='small'
                          onClick={() => startOAuthBinding('telegram')}
                          disabled={bindingPending}
                        >
                          {t('绑定')}
                        </Button>
                      )
                    ) : (
                      <Button
                        disabled
                        size='small'
                        type='primary'
                        theme='outline'
                      >
                        {t('未启用')}
                      </Button>
                    )}
                  </div>
                </div>
              </Card>

              {/* LinuxDO绑定 */}
              <Card className='!rounded-xl'>
                <div className='flex items-center justify-between gap-3'>
                  <div className='flex items-center flex-1 min-w-0'>
                    <div className='w-10 h-10 rounded-full bg-slate-100 dark:bg-slate-700 flex items-center justify-center mr-3 flex-shrink-0'>
                      <SiLinux
                        size={20}
                        className='text-slate-600 dark:text-slate-300'
                      />
                    </div>
                    <div className='flex-1 min-w-0'>
                      <div className='font-medium text-gray-900'>
                        {t('LinuxDO')}
                      </div>
                      <div className='text-sm text-gray-500 truncate'>
                        {renderAccountInfo(
                          userState.user?.linux_do_id,
                          t('LinuxDO ID'),
                        )}
                      </div>
                    </div>
                  </div>
                  <div className='flex-shrink-0'>
                    <Button
                      type='primary'
                      theme='outline'
                      size='small'
                      onClick={() => startOAuthBinding('linuxdo')}
                      disabled={
                        bindingPending ||
                        isBound(userState.user?.linux_do_id) ||
                        !status.linuxdo_oauth
                      }
                    >
                      {status.linuxdo_oauth ? t('绑定') : t('未启用')}
                    </Button>
                  </div>
                </div>
              </Card>

              {/* 自定义 OAuth 提供商绑定 */}
              {status.custom_oauth_providers &&
                status.custom_oauth_providers.map((provider) => {
                  const bound = isCustomOAuthBound(provider.id);
                  const binding = getCustomOAuthBinding(provider.id);
                  return (
                    <Card key={provider.slug} className='!rounded-xl'>
                      <div className='flex items-center justify-between gap-3'>
                        <div className='flex items-center flex-1 min-w-0'>
                          <div className='w-10 h-10 rounded-full bg-slate-100 dark:bg-slate-700 flex items-center justify-center mr-3 flex-shrink-0'>
                            {getOAuthProviderIcon(
                              provider.icon || binding?.provider_icon || '',
                              20,
                            )}
                          </div>
                          <div className='flex-1 min-w-0'>
                            <div className='font-medium text-gray-900'>
                              {provider.name}
                            </div>
                            <div className='text-sm text-gray-500 truncate'>
                              {bound
                                ? renderAccountInfo(
                                    binding?.provider_user_id,
                                    t('{{name}} ID', { name: provider.name }),
                                  )
                                : t('未绑定')}
                            </div>
                          </div>
                        </div>
                        <div className='flex-shrink-0'>
                          {bound ? (
                            <Button
                              type='danger'
                              theme='outline'
                              size='small'
                              disabled={bindingPending}
                              onClick={() =>
                                handleUnbindCustomOAuth(
                                  provider.id,
                                  provider.name,
                                )
                              }
                            >
                              {t('解绑')}
                            </Button>
                          ) : (
                            <Button
                              type='primary'
                              theme='outline'
                              size='small'
                              onClick={() => handleBindCustomOAuth(provider)}
                              disabled={bindingPending}
                            >
                              {t('绑定')}
                            </Button>
                          )}
                        </div>
                      </div>
                    </Card>
                  );
                })}
            </div>
          </div>
        </TabPane>

        {/* 安全设置 Tab */}
        <TabPane
          tab={
            <div className='flex items-center'>
              <ShieldCheck size={16} className='mr-2' />
              {t('安全设置')}
            </div>
          }
          itemKey='security'
        >
          <div className='py-4'>
            <div className='space-y-6'>
              <Space vertical className='w-full'>
                {/* 系统访问令牌 */}
                <Card className='!rounded-xl w-full'>
                  <div className='flex flex-col sm:flex-row items-start sm:justify-between gap-4'>
                    <div className='flex items-start w-full sm:w-auto'>
                      <div className='w-12 h-12 rounded-full bg-slate-100 flex items-center justify-center mr-4 flex-shrink-0'>
                        <IconKey size='large' className='text-slate-600' />
                      </div>
                      <div className='flex-1'>
                        <Typography.Title heading={6} className='mb-1'>
                          {t('Access tokens')}
                        </Typography.Title>
                        <Typography.Text type='tertiary' className='text-sm'>
                          {t('用于API调用的身份验证令牌，请妥善保管')}
                        </Typography.Text>
                        {systemToken && (
                          <div className='mt-3'>
                            <Input
                              readonly
                              value={systemToken}
                              onClick={handleSystemTokenClick}
                              size='large'
                              prefix={<IconKey />}
                            />
                          </div>
                        )}
                      </div>
                    </div>
                    <Button
                      type='primary'
                      theme='solid'
                      onClick={openTokenForm}
                      loading={tokenCatalogLoading}
                      disabled={securityPending || tokenCatalogLoading}
                      className='!bg-slate-600 hover:!bg-slate-700 w-full sm:w-auto'
                      icon={<IconKey />}
                    >
                      {t('Create access token')}
                    </Button>
                  </div>
                </Card>

                {/* 密码管理 */}
                <Card className='!rounded-xl w-full'>
                  <div className='flex flex-col sm:flex-row items-start sm:justify-between gap-4'>
                    <div className='flex items-start w-full sm:w-auto'>
                      <div className='w-12 h-12 rounded-full bg-slate-100 flex items-center justify-center mr-4 flex-shrink-0'>
                        <IconLock size='large' className='text-slate-600' />
                      </div>
                      <div>
                        <Typography.Title heading={6} className='mb-1'>
                          {t('密码管理')}
                        </Typography.Title>
                        <Typography.Text type='tertiary' className='text-sm'>
                          {t('定期更改密码可以提高账户安全性')}
                        </Typography.Text>
                      </div>
                    </div>
                    <Button
                      type='primary'
                      theme='solid'
                      onClick={() => setShowChangePasswordModal(true)}
                      className='!bg-slate-600 hover:!bg-slate-700 w-full sm:w-auto'
                      icon={<IconLock />}
                    >
                      {t('修改密码')}
                    </Button>
                  </div>
                </Card>

                {/* Passkey 设置 */}
                <Card className='!rounded-xl w-full'>
                  <div className='flex flex-col sm:flex-row items-start sm:justify-between gap-4'>
                    <div className='flex items-start w-full sm:w-auto'>
                      <div className='w-12 h-12 rounded-full bg-slate-100 flex items-center justify-center mr-4 flex-shrink-0'>
                        <IconKey size='large' className='text-slate-600' />
                      </div>
                      <div>
                        <Typography.Title heading={6} className='mb-1'>
                          {t('Passkey 登录')}
                        </Typography.Title>
                        <Typography.Text type='tertiary' className='text-sm'>
                          {passkeyEnabled
                            ? t('已启用 Passkey，无需密码即可登录')
                            : t('使用 Passkey 实现免密且更安全的登录体验')}
                        </Typography.Text>
                        <div className='mt-2 text-xs text-gray-500 space-y-1'>
                          <div>
                            {t('最后使用时间')}：{lastUsedLabel}
                          </div>
                          {/*{passkeyEnabled && (*/}
                          {/*  <div>*/}
                          {/*    {t('备份支持')}：*/}
                          {/*    {passkeyStatus?.backup_eligible*/}
                          {/*      ? t('支持备份')*/}
                          {/*      : t('不支持')}*/}
                          {/*    ，{t('备份状态')}：*/}
                          {/*    {passkeyStatus?.backup_state ? t('已备份') : t('未备份')}*/}
                          {/*  </div>*/}
                          {/*)}*/}
                          {!passkeySupported && (
                            <div className='text-amber-600'>
                              {t('当前设备不支持 Passkey')}
                            </div>
                          )}
                        </div>
                      </div>
                    </div>
                    <Button
                      type={passkeyEnabled ? 'danger' : 'primary'}
                      theme={passkeyEnabled ? 'solid' : 'solid'}
                      onClick={
                        passkeyEnabled
                          ? () => {
                              Modal.confirm({
                                title: t('确认解绑 Passkey'),
                                content: t(
                                  '解绑后将无法使用 Passkey 登录，确定要继续吗？',
                                ),
                                okText: t('确认解绑'),
                                cancelText: t('取消'),
                                okType: 'danger',
                                onOk: onPasskeyDelete,
                              });
                            }
                          : onPasskeyRegister
                      }
                      className={`w-full sm:w-auto ${passkeyEnabled ? '!bg-slate-500 hover:!bg-slate-600' : ''}`}
                      icon={<IconKey />}
                      disabled={!passkeySupported && !passkeyEnabled}
                      loading={
                        passkeyEnabled
                          ? passkeyDeleteLoading
                          : passkeyRegisterLoading
                      }
                    >
                      {passkeyEnabled ? t('解绑 Passkey') : t('注册 Passkey')}
                    </Button>
                  </div>
                </Card>

                {/* 两步验证设置 */}
                <TwoFASetting t={t} />

                {/* 危险区域 */}
                <Card className='!rounded-xl w-full'>
                  <div className='flex flex-col sm:flex-row items-start sm:justify-between gap-4'>
                    <div className='flex items-start w-full sm:w-auto'>
                      <div className='w-12 h-12 rounded-full bg-slate-100 flex items-center justify-center mr-4 flex-shrink-0'>
                        <IconDelete size='large' className='text-slate-600' />
                      </div>
                      <div>
                        <Typography.Title
                          heading={6}
                          className='mb-1 text-slate-700'
                        >
                          {t('删除账户')}
                        </Typography.Title>
                        <Typography.Text type='tertiary' className='text-sm'>
                          {t('此操作不可逆，所有数据将被永久删除')}
                        </Typography.Text>
                      </div>
                    </div>
                    <Button
                      type='danger'
                      theme='solid'
                      onClick={() => setShowAccountDeleteModal(true)}
                      className='w-full sm:w-auto !bg-slate-500 hover:!bg-slate-600'
                      icon={<IconDelete />}
                    >
                      {t('删除账户')}
                    </Button>
                  </div>
                </Card>
              </Space>
            </div>
          </div>
        </TabPane>
      </Tabs>
      <Modal
        title={t('Create access token')}
        visible={tokenFormOpen}
        onCancel={() => setTokenFormOpen(false)}
        onOk={createSelectedToken}
        okText={t('Create access token')}
        cancelText={t('取消')}
        okButtonProps={{
          disabled: securityPending || !tokenName.trim() || !tokenScopes.length,
        }}
      >
        <div className='space-y-4'>
          <div>
            <label htmlFor='classic-access-token-name'>{t('Token name')}</label>
            <Input
              id='classic-access-token-name'
              value={tokenName}
              onChange={setTokenName}
              autoComplete='off'
            />
          </div>
          <fieldset>
            <legend>{t('Expiration')}</legend>
            <RadioGroup
              value={tokenExpiryDays}
              onChange={(event) => setTokenExpiryDays(event.target.value)}
            >
              {tokenExpiryOptions.map((days) => (
                <Radio key={days} value={days}>
                  {t('Valid for {{days}} days', { days })}
                </Radio>
              ))}
            </RadioGroup>
          </fieldset>
          <fieldset>
            <legend>{t('Permissions')}</legend>
            <Checkbox.Group
              direction='vertical'
              value={tokenScopes}
              onChange={setTokenScopes}
              options={tokenScopeOptions}
            />
          </fieldset>
        </div>
      </Modal>
      <Modal
        title={t('确认解绑')}
        visible={Boolean(unbindTarget)}
        onCancel={() => setUnbindTarget(null)}
        onOk={confirmOAuthUnbinding}
        okText={t('确认')}
        cancelText={t('取消')}
      >
        {t('确定要解绑 {{name}} 吗？', {
          name: unbindTarget?.providerName || '',
        })}
      </Modal>
      <SecureVerificationModal
        visible={security.isModalVisible}
        verificationMethods={security.verificationMethods}
        verificationState={security.verificationState}
        onVerify={security.executeVerification}
        onCancel={cancelBinding}
        onCodeChange={security.setVerificationCode}
        onPasswordChange={security.setVerificationPassword}
        onProviderChange={security.setVerificationProvider}
        onMethodSwitch={security.switchVerificationMethod}
      />
      <Modal
        title={t('账户绑定')}
        visible={Boolean(preparedBinding)}
        onCancel={cancelBinding}
        onOk={completeOAuthBinding}
        okText={t('继续')}
        cancelText={t('取消')}
      >
        {t('验证成功！')}
      </Modal>
    </Card>
  );
};

export default AccountManagement;
