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
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  completeLogin,
  isLoginChallenge,
  loginWithPasskey,
  verifyLoginChallenge,
} from '../loginFlow';
import { API } from '../../../helpers';
import { getServerMessage, showError } from '../../../helpers/utils';
import { Toast } from '@douyinfe/semi-ui';
import i18next from 'i18next';
import en from '../../../i18n/locales/en.json';
import zh from '../../../i18n/locales/zh-CN.json';

vi.mock('../../../helpers', () => ({
  API: { post: vi.fn(), get: vi.fn() },
  updateAPI: vi.fn(),
}));

const bundle = {
  user: { id: 7, username: 'member', role: 1 },
  access_token: 'access-secret',
  token_type: 'Bearer',
  access_expires_at: 4102444800,
  session: { sid: 'session-secret', current: true },
};
const challenge = {
  require_verification: true,
  flow_token: 'login-challenge',
  expires_at: 4102444800,
  methods: [
    { method: '2fa', available: true },
    { method: 'passkey', available: true },
  ],
};
const passkeyOptions = {
  flow_token: 'passkey-challenge',
  expires_at: 4102444800,
  options: { publicKey: { challenge: 'Y2hhbGxlbmdl', rpId: 'localhost' } },
};
const ok = (data) => ({ data: { success: true, data } });

beforeEach(() => {
  localStorage.clear();
  API.post.mockReset();
  Object.defineProperty(navigator, 'credentials', {
    configurable: true,
    value: {
      get: vi.fn().mockResolvedValue({
        id: 'credential-id',
        rawId: new Uint8Array([1]).buffer,
        type: 'public-key',
        response: {
          authenticatorData: new Uint8Array([2]).buffer,
          clientDataJSON: new Uint8Array([3]).buffer,
          signature: new Uint8Array([4]).buffer,
        },
      }),
    },
  });
});

describe('Classic login contracts', () => {
  it('translates typed errors with display-ready quota parameters without exposing the Axios message', async () => {
    await i18next.init({
      lng: 'zh-CN',
      keySeparator: false,
      resources: { en, 'zh-CN': zh },
    });
    const body = {
      message: 'Email binding bonus $1',
      message_key: 'Email binding bonus {{quota}}',
      message_params: { quota: '$1' },
    };
    expect(getServerMessage(body)).toBe('绑定邮箱赠送 $1');
    expect(getServerMessage('provider text {{quota}}')).toBe(
      'provider text {{quota}}',
    );
    const errorToast = vi.spyOn(Toast, 'error').mockImplementation(() => {});
    showError({
      name: 'AxiosError',
      message: 'opaque transport failure',
      response: { status: 400, data: body },
    });
    expect(errorToast).toHaveBeenCalledWith('错误：绑定邮箱赠送 $1');
    errorToast.mockRestore();
  });

  it('does not establish a session from a pending or malformed challenge', () => {
    const dispatch = vi.fn();
    expect(isLoginChallenge(challenge)).toBe(true);
    expect(isLoginChallenge({ ...challenge, methods: [] })).toBe(false);
    expect(() => completeLogin(challenge, dispatch)).toThrow();
    expect(dispatch).not.toHaveBeenCalled();
    expect(localStorage.getItem('user')).toBeNull();
  });

  it('stores and dispatches only user metadata after accepting an auth bundle', () => {
    const dispatch = vi.fn();
    completeLogin(bundle, dispatch);
    expect(dispatch).toHaveBeenCalledWith({
      type: 'login',
      payload: { ...bundle.user, session: { sid: 'session-secret' } },
    });
    expect(JSON.parse(localStorage.getItem('user'))).toEqual({
      ...bundle.user,
      session: { sid: 'session-secret' },
    });
    expect(JSON.stringify(localStorage)).not.toContain('access-secret');
  });

  it.each(['123456', 'AbCd1234'])(
    'continues login using the flow and code %s',
    async (code) => {
      API.post.mockResolvedValue(ok(bundle));
      const signal = new AbortController().signal;
      await expect(
        verifyLoginChallenge(challenge, '2fa', code, signal),
      ).resolves.toEqual(bundle);
      expect(API.post).toHaveBeenCalledWith(
        '/api/user/login/verify',
        {
          method: '2fa',
          flow_token: 'login-challenge',
          code,
        },
        expect.objectContaining({
          signal,
          skipAuthRefresh: true,
          singleUseAuthorization: true,
        }),
      );
      expect(localStorage.getItem('user')).toBeNull();
    },
  );

  it('rejects expired challenges and server-disallowed verification methods without requests', async () => {
    await expect(
      verifyLoginChallenge({ ...challenge, expires_at: 1 }, '2fa', '123456'),
    ).rejects.toThrow();
    await expect(
      verifyLoginChallenge(
        { ...challenge, methods: [{ method: '2fa', available: false }] },
        '2fa',
        '123456',
      ),
    ).rejects.toThrow();
    expect(API.post).not.toHaveBeenCalled();
  });

  it('binds standalone Passkey completion to the begin flow and credential', async () => {
    API.post
      .mockResolvedValueOnce(ok(passkeyOptions))
      .mockResolvedValueOnce(ok(bundle));
    await expect(
      loginWithPasskey(new AbortController().signal),
    ).resolves.toEqual(bundle);
    expect(API.post).toHaveBeenLastCalledWith(
      '/api/user/passkey/login/finish',
      {
        flow_token: 'passkey-challenge',
        credential: expect.objectContaining({ id: 'credential-id' }),
      },
      expect.objectContaining({
        singleUseAuthorization: true,
        skipAuthRefresh: true,
      }),
    );
  });

  it('uses a selected historical RP verbatim and remembers it only after successful login', async () => {
    const rpID = 'WWW.example.com';
    const onDomains = vi.fn();
    API.post
      .mockResolvedValueOnce(
        ok({
          ...passkeyOptions,
          options: {
            publicKey: {
              challenge: 'Y2hhbGxlbmdl',
              rpId: rpID,
              userVerification: 'required',
            },
          },
          rp_ids: ['example.com', rpID],
        }),
      )
      .mockResolvedValueOnce(ok(bundle));
    await expect(
      loginWithPasskey(undefined, undefined, { rpID, onDomains }),
    ).resolves.toEqual(bundle);
    expect(API.post).toHaveBeenNthCalledWith(
      1,
      '/api/user/passkey/login/begin',
      { rp_id: rpID },
      expect.any(Object),
    );
    expect(onDomains).toHaveBeenCalledWith({
      rpID,
      rpIDs: ['example.com', rpID],
    });
    expect(navigator.credentials.get).toHaveBeenCalledWith(
      expect.objectContaining({
        publicKey: expect.objectContaining({
          rpId: rpID,
          userVerification: 'required',
        }),
      }),
    );
    expect(localStorage.getItem('passkey:last-successful-rp-id')).toBe(rpID);
  });

  it('clears an unavailable remembered RP before one default begin, without retrying a browser prompt', async () => {
    localStorage.setItem('passkey:last-successful-rp-id', 'old.example.com');
    API.post
      .mockResolvedValueOnce({
        data: { success: false, code: 'PASSKEY_RP_ID_UNAVAILABLE' },
      })
      .mockResolvedValueOnce(ok(passkeyOptions))
      .mockResolvedValueOnce(ok(bundle));
    await expect(loginWithPasskey()).resolves.toEqual(bundle);
    expect(API.post).toHaveBeenNthCalledWith(
      1,
      '/api/user/passkey/login/begin',
      { rp_id: 'old.example.com' },
      expect.any(Object),
    );
    expect(API.post).toHaveBeenNthCalledWith(
      2,
      '/api/user/passkey/login/begin',
      undefined,
      expect.any(Object),
    );
    expect(navigator.credentials.get).toHaveBeenCalledOnce();
    expect(localStorage.getItem('passkey:last-successful-rp-id')).toBe(
      'localhost',
    );
  });

  it('does not fall back from an explicitly selected unavailable RP', async () => {
    API.post.mockResolvedValueOnce({
      data: { success: false, code: 'PASSKEY_RP_ID_UNAVAILABLE' },
    });
    await expect(
      loginWithPasskey(undefined, undefined, { rpID: 'old.example.com' }),
    ).rejects.toThrow();
    expect(API.post).toHaveBeenCalledOnce();
    expect(navigator.credentials.get).not.toHaveBeenCalled();
  });

  it('does not remember an RP after rejected finish or issue requests for an aborted operation', async () => {
    API.post
      .mockResolvedValueOnce(ok(passkeyOptions))
      .mockRejectedValueOnce(new Error('rejected'));
    await expect(loginWithPasskey()).rejects.toThrow();
    expect(localStorage.getItem('passkey:last-successful-rp-id')).toBeNull();
    API.post.mockReset();
    const controller = new AbortController();
    controller.abort();
    await expect(loginWithPasskey(controller.signal)).rejects.toThrow();
    expect(API.post).not.toHaveBeenCalled();
  });

  it('binds Passkey second-factor completion to both login and Passkey flows', async () => {
    API.post
      .mockResolvedValueOnce(ok(passkeyOptions))
      .mockResolvedValueOnce(ok(bundle));
    await verifyLoginChallenge(
      challenge,
      'passkey',
      '',
      new AbortController().signal,
    );
    expect(API.post).toHaveBeenNthCalledWith(
      1,
      '/api/user/login/passkey/begin',
      {
        flow_token: 'login-challenge',
      },
      expect.any(Object),
    );
    expect(API.post).toHaveBeenLastCalledWith(
      '/api/user/login/passkey/finish',
      {
        flow_token: 'login-challenge',
        passkey_flow_token: 'passkey-challenge',
        credential: expect.objectContaining({ id: 'credential-id' }),
      },
      expect.any(Object),
    );
  });

  it('does not finish cancelled Passkey ceremonies or retry a rejected completion', async () => {
    const controller = new AbortController();
    API.post.mockResolvedValueOnce(ok(passkeyOptions));
    navigator.credentials.get.mockImplementationOnce(async () => {
      controller.abort();
      return null;
    });
    await expect(loginWithPasskey(controller.signal)).rejects.toThrow();
    expect(API.post).toHaveBeenCalledTimes(1);
    API.post
      .mockReset()
      .mockResolvedValueOnce(ok(passkeyOptions))
      .mockRejectedValueOnce(new Error('rejected'));
    await expect(
      loginWithPasskey(new AbortController().signal),
    ).rejects.toThrow('rejected');
    expect(API.post).toHaveBeenCalledTimes(2);
  });
});
