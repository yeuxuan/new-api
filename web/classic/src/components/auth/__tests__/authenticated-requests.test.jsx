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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
} from '@testing-library/react';
import { useApiRequest } from '../../../hooks/playground/useApiRequest';
import OllamaModelModal from '../../table/channels/modals/OllamaModelModal';
import { API, getFreshAuthHeaders, showError } from '../../../helpers';
import { MESSAGE_STATUS } from '../../../constants/playground.constants';
import { SSE } from 'sse.js';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key) => key }),
}));
vi.mock('../../../helpers', () => ({
  API: {
    getUri: ({ url }) => `https://gateway.test${url}`,
    get: vi.fn(),
    post: vi.fn(),
  },
  getFreshAuthHeaders: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  handleApiError: (error) => ({ message: error.message }),
  processThinkTags: (content, reasoningContent) => ({
    content,
    reasoningContent,
  }),
  processIncompleteThinkTags: (content, reasoningContent) => ({
    content,
    reasoningContent,
  }),
}));
vi.mock('sse.js', () => ({
  SSE: vi.fn(function () {
    this.listeners = {};
    this.addEventListener = (name, callback) => {
      this.listeners[name] = callback;
    };
    this.stream = vi.fn();
    this.close = vi.fn();
  }),
}));

const headers = {
  'Content-Type': 'application/json',
  Authorization: 'Bearer fresh-access',
  'New-API-User': '7',
};
const payload = {
  model: 'test-model',
  messages: [{ role: 'user', content: 'hello' }],
};

function renderRequest() {
  let messages = [
    { role: 'assistant', status: MESSAGE_STATUS.LOADING, content: '' },
  ];
  let debug = {};
  const source = { current: null };
  const { result } = renderHook(() =>
    useApiRequest(
      (update) => {
        messages = update(messages);
      },
      (update) => {
        debug = update(debug);
      },
      vi.fn(),
      source,
      vi.fn(),
    ),
  );
  return { result, source, messages: () => messages, debug: () => debug };
}

beforeEach(() => {
  getFreshAuthHeaders.mockReset().mockResolvedValue(headers);
  API.get.mockReset().mockResolvedValue({ data: { success: true, data: [] } });
  API.post.mockReset().mockResolvedValue({ data: { success: true, data: [] } });
  vi.stubGlobal('fetch', vi.fn());
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('Classic authenticated raw requests', () => {
  it('sends a fresh bearer token and cookies on nonstream playground requests', async () => {
    fetch.mockResolvedValue({
      ok: true,
      json: async () => ({ choices: [{ message: { content: 'answer' } }] }),
    });
    const request = renderRequest();
    await request.result.current.sendRequest(payload, false);
    expect(fetch).toHaveBeenCalledWith(
      'https://gateway.test/pg/chat/completions',
      {
        method: 'POST',
        headers,
        credentials: 'include',
        body: JSON.stringify(payload),
      },
    );
    expect(request.messages()[0]).toMatchObject({
      content: 'answer',
      status: MESSAGE_STATUS.COMPLETE,
    });
  });

  it('authenticates SSE and still delivers content and completion', async () => {
    const request = renderRequest();
    await request.result.current.sendRequest(payload, true);
    expect(SSE).toHaveBeenCalledWith(
      'https://gateway.test/pg/chat/completions',
      {
        headers,
        withCredentials: true,
        method: 'POST',
        payload: JSON.stringify(payload),
      },
    );
    const source = request.source.current;
    expect(source.stream).toHaveBeenCalledOnce();
    source.listeners.message({
      data: JSON.stringify({ choices: [{ delta: { content: 'answer' } }] }),
    });
    source.listeners.message({ data: '[DONE]' });
    expect(request.messages()[0]).toMatchObject({
      content: 'answer',
      status: MESSAGE_STATUS.COMPLETE,
    });
    expect(source.close).toHaveBeenCalledOnce();
    expect(request.source.current).toBeNull();
  });

  it('does not start a billable SSE request after stopping during token refresh', async () => {
    let resolve;
    getFreshAuthHeaders.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const request = renderRequest();
    const pending = request.result.current.sendRequest(payload, true);
    request.result.current.onStopGenerator();
    resolve(headers);
    await pending;
    expect(SSE).not.toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();
    expect(request.source.current).toBeNull();
    expect(request.messages()[0].status).toBe(MESSAGE_STATUS.COMPLETE);
    expect(request.debug().isStreaming).toBe(false);
  });

  it.each([false, true])(
    'fails closed when authentication cannot be refreshed (stream=%s)',
    async (stream) => {
      vi.spyOn(console, 'error').mockImplementation(() => {});
      getFreshAuthHeaders.mockRejectedValue(new Error('Session expired'));
      const request = renderRequest();
      await request.result.current.sendRequest(payload, stream);
      expect(SSE).not.toHaveBeenCalled();
      expect(fetch).not.toHaveBeenCalled();
      expect(request.messages()[0].status).toBe(MESSAGE_STATUS.ERROR);
      expect(request.debug().isStreaming).toBe(false);
    },
  );

  it('authenticates the Ollama model pull button with the current session', async () => {
    fetch.mockResolvedValue({
      ok: true,
      body: { getReader: () => ({ read: async () => ({ done: true }) }) },
    });
    render(<OllamaModelModal visible channelId={3} onCancel={vi.fn()} />);
    fireEvent.change(
      screen.getByPlaceholderText('请输入模型名称，例如: llama3.2, qwen2.5:7b'),
      { target: { value: 'test-model' } },
    );
    fireEvent.click(screen.getByRole('button', { name: /拉取模型/ }));
    await waitFor(() => expect(fetch).toHaveBeenCalledOnce());
    expect(fetch).toHaveBeenCalledWith(
      'https://gateway.test/api/channel/ollama/pull/stream',
      expect.objectContaining({
        headers: { Accept: 'text/event-stream', ...headers },
        credentials: 'include',
        method: 'POST',
        body: JSON.stringify({ channel_id: 3, model_name: 'test-model' }),
      }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /拉取模型/ }),
      ).not.toBeDisabled(),
    );
    expect(showError).not.toHaveBeenCalled();
  });

  it('cancels an Ollama pull when the dialog unmounts during refresh', async () => {
    let resolve;
    getFreshAuthHeaders.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const view = render(
      <OllamaModelModal visible channelId={3} onCancel={vi.fn()} />,
    );
    fireEvent.change(
      screen.getByPlaceholderText('请输入模型名称，例如: llama3.2, qwen2.5:7b'),
      { target: { value: 'test-model' } },
    );
    fireEvent.click(screen.getByRole('button', { name: /拉取模型/ }));
    view.unmount();
    await act(async () => {
      resolve(headers);
    });
    expect(fetch).not.toHaveBeenCalled();
    expect(showError).not.toHaveBeenCalled();
  });
});
