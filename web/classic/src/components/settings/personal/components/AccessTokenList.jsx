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
import { Button, Checkbox, Input, Modal, Typography } from '@douyinfe/semi-ui';
import { Pencil, RotateCw, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  API,
  getServerMessage,
  showError,
  timestamp2string,
} from '../../../../helpers';

export default function AccessTokenList(props) {
  const { t } = useTranslation();
  const [list, setList] = React.useState(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState('');
  const [retry, setRetry] = React.useState(0);
  const [editing, setEditing] = React.useState(null);
  const [revokeTarget, setRevokeTarget] = React.useState(null);
  const [catalogLoading, setCatalogLoading] = React.useState(false);
  const [options, setOptions] = React.useState([]);
  const [name, setName] = React.useState('');
  const [scopes, setScopes] = React.useState([]);
  const [initialScopes, setInitialScopes] = React.useState([]);
  const catalogRequest = React.useRef(null);
  const translate = React.useRef(t);
  translate.current = t;

  React.useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setList(null);
    setError('');
    (async () => {
      try {
        const res = await API.get('/api/user/access_tokens', {
          skipErrorHandler: true,
          signal: controller.signal,
        });
        controller.signal.throwIfAborted();
        if (!res.data.success || !Array.isArray(res.data.data?.items)) {
          throw new Error(
            getServerMessage(res.data, translate.current) ||
              translate.current('Failed to load access tokens'),
          );
        }
        setList(res.data.data);
      } catch (failure) {
        if (!controller.signal.aborted) {
          setError(
            getServerMessage(failure.response?.data, translate.current) ||
              failure.message ||
              translate.current('Failed to load access tokens'),
          );
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    })();
    return () => controller.abort();
  }, [props.userId, props.sessionId, props.revision, retry]);

  React.useEffect(() => {
    setEditing(null);
    setRevokeTarget(null);
    setName('');
    setScopes([]);
    setOptions([]);
    setCatalogLoading(false);
    return () => {
      catalogRequest.current?.abort();
      catalogRequest.current = null;
    };
  }, [props.userId, props.sessionId]);

  const openEditor = async (token) => {
    if (props.pending || catalogRequest.current) return;
    const controller = new AbortController();
    catalogRequest.current = controller;
    setCatalogLoading(true);
    try {
      const res = await API.get('/api/user/access_tokens/catalog', {
        skipErrorHandler: true,
        signal: controller.signal,
      });
      controller.signal.throwIfAborted();
      if (!res.data.success || !Array.isArray(res.data.data?.groups)) {
        throw new Error(
          getServerMessage(res.data, t) || t('Failed to load access tokens'),
        );
      }
      const offered = res.data.data.groups.flatMap((group) =>
        (Array.isArray(group.resources) ? group.resources : []).flatMap(
          (resource) =>
            (Array.isArray(resource.actions) ? resource.actions : []).map(
              (action) => ({
                value: `${resource.resource}:${action.action}`,
                resourceLabel: resource.label_key,
                actionLabel: action.label_key,
              }),
            ),
        ),
      );
      const allowed = new Set(offered.map((option) => option.value));
      const initial = (token.scopes || [])
        .filter((scope) => allowed.has(scope))
        .sort();
      setOptions(offered);
      setInitialScopes(initial);
      setScopes(initial);
      setName(token.name);
      setEditing(token);
    } catch (failure) {
      if (!controller.signal.aborted) showError(failure);
    } finally {
      if (catalogRequest.current === controller) {
        catalogRequest.current = null;
        setCatalogLoading(false);
      }
    }
  };

  const sortedScopes = [...new Set(scopes)].sort();
  const permissionsChanged = sortedScopes.join(' ') !== initialScopes.join(' ');
  const validName = name.trim() && [...name.trim()].length <= 64;
  const save = () => {
    if (
      !editing ||
      props.pending ||
      !validName ||
      (permissionsChanged && !scopes.length)
    )
      return;
    const allowed = new Set(options.map((option) => option.value));
    if (sortedScopes.some((scope) => !allowed.has(scope))) return;
    props.onUpdate(
      editing.id,
      {
        name: name.trim(),
        ...(permissionsChanged ? { scopes: sortedScopes } : {}),
      },
      () => setEditing(null),
    );
  };

  const confirmRevoke = () => {
    if (!revokeTarget || props.pending) return;
    const target = revokeTarget.id;
    setRevokeTarget(null);
    props.onRevoke(target);
  };

  let content;
  if (loading) {
    content = <div role='status'>{t('Loading...')}</div>;
  } else if (error || !list) {
    content = (
      <div role='alert' className='flex flex-wrap items-center gap-3'>
        <Typography.Text type='danger'>
          {error || t('Failed to load access tokens')}
        </Typography.Text>
        <Button
          icon={<RotateCw size={14} />}
          onClick={() => setRetry((value) => value + 1)}
        >
          {t('Retry')}
        </Button>
      </div>
    );
  } else {
    const tokens = [...list.items];
    if (list.legacy)
      tokens.unshift({
        ...list.legacy,
        id: 'legacy',
        name: t('Legacy token'),
        scopes: [],
      });
    content = tokens.length ? (
      <ul className='m-0 list-none p-0 divide-y'>
        {tokens.map((token) => (
          <li
            key={token.id}
            className='flex flex-wrap items-start justify-between gap-3 py-3'
          >
            <div className='min-w-0 flex-1 space-y-1'>
              <div className='break-words font-medium'>{token.name}</div>
              <code className='text-xs'>...{token.token_hint}</code>
              <div className='flex flex-wrap gap-x-3 gap-y-1 text-xs text-gray-500'>
                {token.expires_at > 0 && (
                  <span>
                    {t('Expires {{time}}', {
                      time: timestamp2string(token.expires_at),
                    })}
                  </span>
                )}
                {token.expires_at > 0 &&
                  token.expires_at * 1000 <= Date.now() && (
                    <span>{t('Expired')}</span>
                  )}
                {token.created_at > 0 && (
                  <span>
                    {t('Created {{time}}', {
                      time: timestamp2string(token.created_at),
                    })}
                  </span>
                )}
                <span>
                  {token.last_used_at
                    ? t('Last used {{time}}', {
                        time: timestamp2string(token.last_used_at),
                      })
                    : t('Never used')}
                </span>
              </div>
              {token.scopes?.length > 0 && (
                <div className='break-words text-xs'>
                  {token.scopes.join(', ')}
                </div>
              )}
              {token.id === 'legacy' && (
                <div className='break-words text-sm'>
                  {t(
                    'The legacy token stops working on {{date}}. Create a new token to replace it.',
                    { date: timestamp2string(token.retire_at) },
                  )}
                </div>
              )}
            </div>
            <div className='flex shrink-0 gap-2'>
              {token.id !== 'legacy' && (
                <Button
                  aria-label={t('Edit {{name}}', { name: token.name })}
                  icon={<Pencil size={14} />}
                  disabled={props.pending || catalogLoading}
                  onClick={() => openEditor(token)}
                >
                  {t('Edit')}
                </Button>
              )}
              <Button
                aria-label={t('Revoke {{name}}', { name: token.name })}
                icon={<Trash2 size={14} />}
                type='danger'
                disabled={props.pending}
                onClick={() =>
                  setRevokeTarget({ id: token.id, name: token.name })
                }
              >
                {t('Revoke')}
              </Button>
            </div>
          </li>
        ))}
      </ul>
    ) : (
      <Typography.Text type='tertiary'>{t('No access tokens')}</Typography.Text>
    );
  }

  return (
    <div className='mt-4 border-t pt-3'>
      {content}
      <Modal
        title={t('Edit access token')}
        visible={Boolean(editing) && !props.pending}
        onCancel={() => {
          if (!props.pending) setEditing(null);
        }}
        onOk={save}
        okText={t('Save')}
        cancelText={t('取消')}
        cancelButtonProps={{ 'aria-label': t('取消') }}
        okButtonProps={{
          'aria-label': t('Save'),
          disabled:
            props.pending ||
            !validName ||
            (permissionsChanged && !scopes.length),
        }}
      >
        <div className='space-y-4'>
          <div>
            <label htmlFor='classic-access-token-edit-name'>
              {t('Token name')}
            </label>
            <Input
              id='classic-access-token-edit-name'
              value={name}
              onChange={setName}
              autoComplete='off'
            />
          </div>
          <fieldset>
            <legend>{t('Permissions')}</legend>
            <Checkbox.Group
              direction='vertical'
              value={scopes}
              onChange={setScopes}
              options={options.map((option) => ({
                value: option.value,
                label: `${t(option.resourceLabel)} / ${t(option.actionLabel)}`,
              }))}
            />
          </fieldset>
          <Typography.Text type='tertiary'>
            {t('Saving permission changes requires security verification')}
          </Typography.Text>
        </div>
      </Modal>
      <Modal
        title={t('Revoke')}
        visible={Boolean(revokeTarget)}
        onCancel={() => setRevokeTarget(null)}
        onOk={confirmRevoke}
        okText={t('Revoke')}
        cancelText={t('取消')}
        okType='danger'
        okButtonProps={{ 'aria-label': t('Revoke') }}
        cancelButtonProps={{ 'aria-label': t('取消') }}
      >
        <div className='break-words font-medium'>{revokeTarget?.name}</div>
        {t(
          'Revoke this access token? Scripts using it stop working immediately.',
        )}
      </Modal>
    </div>
  );
}
