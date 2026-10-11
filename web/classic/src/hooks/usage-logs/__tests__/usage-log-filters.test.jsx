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
import { useTaskLogsData } from '../../task-logs/useTaskLogsData';
import { getTaskLogsColumns } from '../../../components/table/task-logs/TaskLogsColumnDefs';
import ContentModal from '../../../components/table/task-logs/modals/ContentModal';
import { TASK_ACTION_TEXT_GENERATE } from '../../../constants/common.constant';
import { useLogsData } from '../useUsageLogsData';
import { API, isAdmin, showError } from '../../../helpers';
import { createInstance } from 'i18next';
import en from '../../../i18n/locales/en.json';
import zh from '../../../i18n/locales/zh-CN.json';

const { translate } = vi.hoisted(() => ({ translate: vi.fn((key) => key) }));
const i18n = createInstance();
await i18n.init({
  lng: 'zh-CN',
  keySeparator: false,
  resources: { en, 'zh-CN': zh },
});

describe('Classic task log preview contracts', () => {
  const videoUrl =
    'https://gateway.example/v1/tasks/task-video/artifacts/video/content?access=' +
    'A'.repeat(43);

  it.each([
    ['text_to_video', 'video'],
    ['image_to_video', 'video'],
    ['first_tail_to_video', 'video'],
    ['reference_to_video', 'video'],
    ['remix', 'video'],
    ['text_to_image', 'image'],
    ['speech', 'audio'],
    ['export', 'file'],
  ])(
    'offers protected lazy artifact preview for a successful plugin action %s (%s)',
    async (action, type) => {
      const record = Object.freeze({
        task_id: 'plugin/task',
        platform: '54',
        action,
        status: 'SUCCESS',
        legacy_video_available: false,
      });
      const artifact = { key: 'result', type, content_url: videoUrl };
      const { result } = renderHook(() => useTaskLogsData());
      await waitFor(() => expect(result.current.loading).toBe(false));
      API.get.mockClear().mockResolvedValue({
        data: { success: true, data: { artifacts: [artifact] } },
      });
      const column = getTaskLogsColumns(result.current).find(
        (item) => item.dataIndex === 'fail_reason',
      );
      render(column.render('', record));
      expect(API.get).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole('link', { name: i18n.t('Artifacts') }));
      await waitFor(() =>
        expect(result.current.isArtifactModalOpen).toBe(true),
      );
      expect(API.get).toHaveBeenCalledWith(
        '/api/task/plugin%2Ftask/artifacts',
        {
          skipErrorHandler: true,
        },
      );
      expect(result.current.taskArtifacts).toEqual([artifact]);
      expect(record).not.toHaveProperty('result_url');
      expect(record).not.toHaveProperty('data');
    },
  );

  it('renders every projected media type and file download without embedding files', () => {
    const artifacts = ['image', 'video', 'audio', 'file'].map((type) => ({
      key: `${type}-main`,
      type,
      content_url: videoUrl,
      mime_type: type === 'file' ? 'text/html' : undefined,
    }));
    render(
      <ContentModal
        isModalOpen={true}
        setIsModalOpen={vi.fn()}
        artifacts={artifacts}
      />,
    );
    expect(
      screen.getByRole('img', { name: 'image-main' }).getAttribute('src'),
    ).toBe(videoUrl);
    expect(document.querySelector('video').getAttribute('src')).toBe(videoUrl);
    expect(document.querySelector('audio').getAttribute('src')).toBe(videoUrl);
    expect(document.querySelector('iframe, object, embed')).toBeNull();
    expect(
      screen.getAllByRole('button', { name: i18n.t('Download') }),
    ).toHaveLength(4);
    const modalBody = document.querySelector('.semi-modal-body');
    expect(modalBody.style.overflow).toBe('auto');
    expect(
      document.querySelector('video[autoplay], audio[autoplay]'),
    ).toBeNull();
    fireEvent.click(
      screen.getAllByRole('button', { name: i18n.t('Download') })[3],
    );
    const download = HTMLAnchorElement.prototype.click.mock.contexts.at(-1);
    expect(download.href).toBe(videoUrl);
    expect(download.download).toBe('file-main');
    expect(download.rel).toBe('noopener noreferrer');
    fireEvent.error(document.querySelector('video'));
    expect(screen.getByText(i18n.t('Preview unavailable'))).toBeTruthy();
    expect(
      screen.getAllByRole('button', { name: i18n.t('Download') }),
    ).toHaveLength(4);
  });

  it('resets the legacy video playback failure when the content URL changes', () => {
    const props = { isModalOpen: true, setIsModalOpen: vi.fn(), isVideo: true };
    const { rerender } = render(
      <ContentModal {...props} modalContent={videoUrl} />,
    );
    fireEvent.error(document.querySelector('video'));
    expect(document.querySelector('video')).toBeNull();
    const nextUrl = videoUrl.replace('task-video', 'next-task');
    rerender(<ContentModal {...props} modalContent={nextUrl} />);
    expect(document.querySelector('video').getAttribute('src')).toBe(nextUrl);
  });

  it.each([
    'http://localhost:3000/v1/tasks/task-public/artifacts/video-main/content',
    'https://media.example/gateway/prefix/v1/tasks/task%2Fvideo%3F1/artifacts/video-main/content',
  ])(
    'accepts absolute content URLs with backend-supported prefixes and escaped task IDs (%s)',
    async (path) => {
      const contentUrl = `${path}?access=${'A'.repeat(43)}`;
      const { result } = renderHook(() => useTaskLogsData());
      await waitFor(() => expect(result.current.loading).toBe(false));
      const artifact = {
        key: 'video-main',
        type: 'video',
        content_url: contentUrl,
      };
      API.get.mockResolvedValueOnce({
        data: { success: true, data: { artifacts: [artifact] } },
      });
      await act(async () => {
        await result.current.openArtifactModal({
          task_id: 'task/video?1',
          status: 'SUCCESS',
        });
      });
      expect(result.current.isArtifactModalOpen).toBe(true);
      expect(result.current.taskArtifacts).toEqual([artifact]);
    },
  );

  it.each(['62', '54'])(
    'loads a successful video on click without list URL or data (platform=%s)',
    async (platform) => {
      isAdmin.mockReturnValue(false);
      const record = Object.freeze({
        id: 1,
        task_id: 'task/video?1',
        platform,
        action: TASK_ACTION_TEXT_GENERATE,
        status: 'SUCCESS',
        result_url: '',
      });
      const { result } = renderHook(() => useTaskLogsData());
      await waitFor(() => expect(result.current.loading).toBe(false));
      API.get.mockClear().mockResolvedValue({
        data: {
          success: true,
          data: { artifacts: [], legacy_content_url: videoUrl },
        },
      });
      const column = getTaskLogsColumns(result.current).find(
        (item) => item.dataIndex === 'fail_reason',
      );
      render(column.render('', record));
      expect(API.get).not.toHaveBeenCalled();
      fireEvent.click(
        screen.getByRole('link', { name: i18n.t('点击预览视频') }),
      );
      await waitFor(() => expect(result.current.isVideoModalOpen).toBe(true));
      expect(API.get).toHaveBeenCalledWith(
        '/api/task/task%2Fvideo%3F1/artifacts',
        { skipErrorHandler: true },
      );
      expect(result.current.videoUrl).toBe(videoUrl);
      expect(record.result_url).toBe('');
      expect(record).not.toHaveProperty('data');
    },
  );

  it('shows all projected artifacts when an older preview entry receives a mixed plugin result', async () => {
    const artifacts = [
      { key: 'video-main', type: 'video', content_url: videoUrl },
      { key: 'image-main', type: 'image', content_url: videoUrl },
      { key: 'download', type: 'file', content_url: videoUrl },
    ];
    const { result } = renderHook(() => useTaskLogsData());
    await waitFor(() => expect(result.current.loading).toBe(false));
    API.get.mockClear().mockResolvedValue({
      data: { success: true, data: { artifacts } },
    });
    await act(async () => {
      await result.current.openVideoModal({
        task_id: 'plugin-task',
        status: 'SUCCESS',
      });
    });
    expect(result.current.isArtifactModalOpen).toBe(true);
    expect(result.current.taskArtifacts).toEqual(artifacts);
    expect(result.current.isVideoModalOpen).toBe(false);
  });

  it.each(['business', 'network', 'discarded'])(
    'clears previous preview state when the next artifact request is %s',
    async (failure) => {
      const { result } = renderHook(() => useTaskLogsData());
      await waitFor(() => expect(result.current.loading).toBe(false));
      API.get.mockResolvedValueOnce({
        data: {
          success: true,
          data: {
            artifacts: [{ key: 'result', type: 'file', content_url: videoUrl }],
          },
        },
      });
      await act(async () => {
        await result.current.openArtifactModal({
          task_id: 'old-task',
          status: 'SUCCESS',
        });
      });
      expect(result.current.isArtifactModalOpen).toBe(true);
      API.get.mockClear();
      if (failure === 'business') {
        API.get.mockResolvedValueOnce({
          data: { success: false, message: 'Task not found' },
        });
      } else if (failure === 'network') {
        API.get.mockRejectedValueOnce({
          response: {
            status: 403,
            data: { message: 'Artifact access is invalid' },
          },
        });
      }
      await act(async () => {
        await result.current.openArtifactModal({
          task_id: 'new-task',
          status: 'SUCCESS',
          result_discarded: failure === 'discarded',
        });
      });
      expect(result.current.isArtifactModalOpen).toBe(false);
      expect(result.current.taskArtifacts).toEqual([]);
      expect(result.current.isVideoModalOpen).toBe(false);
      expect(result.current.videoUrl).toBe('');
      expect(result.current.isAudioModalOpen).toBe(false);
      expect(result.current.audioClips).toEqual([]);
      if (failure === 'discarded') expect(API.get).not.toHaveBeenCalled();
      else expect(showError).toHaveBeenCalledOnce();
    },
  );

  it('does not replace the latest preview with a late response from a previous task', async () => {
    const { result } = renderHook(() => useTaskLogsData());
    await waitFor(() => expect(result.current.loading).toBe(false));
    let resolvePrevious;
    API.get.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolvePrevious = resolve;
        }),
    );
    const latestArtifact = {
      key: 'new-result',
      type: 'image',
      content_url: videoUrl,
    };
    API.get.mockResolvedValueOnce({
      data: { success: true, data: { artifacts: [latestArtifact] } },
    });
    await act(async () => {
      const previousRequest = result.current.openArtifactModal({
        task_id: 'previous-task',
        status: 'SUCCESS',
      });
      await result.current.openArtifactModal({
        task_id: 'latest-task',
        status: 'SUCCESS',
      });
      resolvePrevious({
        data: {
          success: true,
          data: {
            artifacts: [
              { key: 'old-result', type: 'file', content_url: videoUrl },
            ],
          },
        },
      });
      await previousRequest;
    });
    expect(result.current.taskArtifacts).toEqual([latestArtifact]);
  });

  it.each(['legacy', 'artifacts'])(
    'loads Suno audio from the %s projection, not list snapshots',
    async (projection) => {
      const record = Object.freeze({
        task_id: 'task-audio',
        platform: 'suno',
        status: 'SUCCESS',
      });
      const clip = {
        audio_url: 'https://media.example/audio.mp3',
        title: 'Song',
      };
      const data =
        projection === 'legacy'
          ? { artifacts: [], legacy_audio_clips: [clip] }
          : {
              artifacts: [
                { key: 'audio-main', type: 'audio', content_url: videoUrl },
              ],
            };
      const { result } = renderHook(() => useTaskLogsData());
      await waitFor(() => expect(result.current.loading).toBe(false));
      API.get.mockClear().mockResolvedValue({ data: { success: true, data } });
      const column = getTaskLogsColumns(result.current).find(
        (item) => item.dataIndex === 'fail_reason',
      );
      render(column.render('', record));
      expect(API.get).not.toHaveBeenCalled();
      fireEvent.click(
        screen.getByRole('link', { name: i18n.t('点击预览音乐') }),
      );
      await waitFor(() => expect(result.current.isAudioModalOpen).toBe(true));
      expect(result.current.audioClips).toEqual(
        projection === 'legacy'
          ? [clip]
          : [{ clip_id: 'audio-main', audio_url: videoUrl }],
      );
      expect(record).not.toHaveProperty('data');
    },
  );

  it('prefers projected video content over the legacy fallback', async () => {
    const { result } = renderHook(() => useTaskLogsData());
    await waitFor(() => expect(result.current.loading).toBe(false));
    API.get.mockResolvedValue({
      data: {
        success: true,
        data: {
          artifacts: [
            { key: 'video-main', type: 'video', content_url: videoUrl },
          ],
          legacy_content_url: 'https://gateway.example/legacy.mp4',
        },
      },
    });
    await act(async () => {
      await result.current.openVideoModal({
        task_id: 'task-video',
        status: 'SUCCESS',
      });
    });
    expect(result.current.videoUrl).toBe(videoUrl);
  });

  it('does not request discarded or unfinished results, even through modal handlers', async () => {
    const { result } = renderHook(() => useTaskLogsData());
    await waitFor(() => expect(result.current.loading).toBe(false));
    API.get.mockClear();
    const record = {
      task_id: 'task-discarded',
      platform: 'suno',
      status: 'SUCCESS',
      result_discarded: true,
    };
    const column = getTaskLogsColumns(result.current).find(
      (item) => item.dataIndex === 'fail_reason',
    );
    render(column.render('', record));
    expect(screen.queryByRole('link')).toBeNull();
    expect(screen.getByText(i18n.t('Result not retained'))).toBeTruthy();
    await act(async () => {
      await result.current.openAudioModal(record);
      await result.current.openVideoModal(record);
      await result.current.openArtifactModal(record);
      await result.current.openVideoModal({
        task_id: 'task-running',
        status: 'IN_PROGRESS',
      });
      await result.current.openArtifactModal({
        task_id: 'task-running',
        status: 'IN_PROGRESS',
      });
    });
    expect(API.get).not.toHaveBeenCalled();
    expect(result.current.isVideoModalOpen).toBe(false);
    expect(result.current.isAudioModalOpen).toBe(false);
    expect(result.current.isArtifactModalOpen).toBe(false);
  });

  it.each(
    [
      [],
      [{ key: 'file', type: 'file', content_url: 'javascript:alert(1)' }],
      [
        {
          key: 'image',
          type: 'image',
          content_url: 'https://media.example/image.png',
        },
      ],
      [{ key: 'video', type: 'video', content_url: `${videoUrl}#fragment` }],
      [{ key: 'unknown', type: 'html', content_url: videoUrl }],
      [
        { key: 'same', type: 'image', content_url: videoUrl },
        { key: 'same', type: 'file', content_url: videoUrl },
      ],
    ].map((artifacts) => ({ artifacts })),
  )(
    'keeps generic preview closed for invalid or empty projections ($artifacts)',
    async ({ artifacts }) => {
      const { result } = renderHook(() => useTaskLogsData());
      await waitFor(() => expect(result.current.loading).toBe(false));
      API.get.mockClear().mockResolvedValue({
        data: { success: true, data: { artifacts } },
      });
      await act(async () => {
        await result.current.openArtifactModal({
          task_id: 'plugin-task',
          status: 'SUCCESS',
        });
      });
      expect(result.current.isArtifactModalOpen).toBe(false);
      expect(showError).toHaveBeenCalledWith(
        i18n.t('Failed to load artifacts'),
      );
    },
  );

  it('translates failure details for both the cell and content modal', () => {
    const openContentModal = vi.fn();
    const column = getTaskLogsColumns({
      t: i18n.t.bind(i18n),
      COLUMN_KEYS: {},
      openContentModal,
    }).find((item) => item.dataIndex === 'fail_reason');
    const reason = 'Task failed';
    render(column.render(reason, { status: 'FAILURE' }));
    fireEvent.click(screen.getByText(i18n.t(reason)));
    expect(openContentModal).toHaveBeenCalledWith(i18n.t(reason));
  });

  it.each(['business', 'network', 'empty', 'unsafe'])(
    'keeps previews closed and reports %s artifact failures',
    async (failure) => {
      const { result } = renderHook(() => useTaskLogsData());
      await waitFor(() => expect(result.current.loading).toBe(false));
      const error = new Error('network unavailable');
      if (failure === 'network') {
        API.get.mockRejectedValueOnce(error);
      } else {
        API.get.mockResolvedValueOnce({
          data:
            failure === 'business'
              ? { success: false, message: 'Task not found' }
              : {
                  success: true,
                  data: {
                    artifacts: [],
                    legacy_content_url:
                      failure === 'unsafe' ? 'javascript:alert(1)' : '',
                  },
                },
        });
      }
      await act(async () => {
        await result.current.openVideoModal({
          task_id: 'task-video',
          status: 'SUCCESS',
        });
      });
      expect(result.current.isVideoModalOpen).toBe(false);
      expect(showError).toHaveBeenCalledOnce();
      if (failure === 'network') expect(showError).toHaveBeenCalledWith(error);
    },
  );
});

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: translate }),
}));
vi.mock('../../../helpers', async () => {
  const { getLogOther } = await import('../../../helpers/log');
  const {
    renderLogContent,
    renderClaudeLogContent,
    renderModelPrice,
    renderClaudeModelPrice,
  } = await import('../../../helpers/render');
  return {
    API: { get: vi.fn() },
    isAdmin: vi.fn(),
    showError: vi.fn(),
    getTodayStartTimestamp: () => 1700000000,
    timestamp2string: (value) => new Date(value * 1000).toISOString(),
    getTableCompactMode: () => false,
    getLogOther,
    renderLogContent,
    renderClaudeLogContent,
    renderModelPrice,
    renderClaudeModelPrice,
  };
});

const special = 'audit+A&B#1 中文/100%?';
const filters = {
  username: special,
  token_name: special,
  model_name: special,
  channel: '7',
  group: special,
  request_id: special,
  logType: '2',
  dateRange: ['2026-09-01T00:00:00Z', '2026-09-02T00:00:00Z'],
};
const stat = { quota: 1234567, rpm: 15, tpm: 1907376 };

beforeEach(async () => {
  await i18n.changeLanguage('zh-CN');
  translate.mockImplementation((key, options) => i18n.t(key, options));
  localStorage.clear();
  API.get.mockReset().mockImplementation(async (url) => {
    const pathname = new URL(url, 'http://localhost').pathname;
    if (pathname.endsWith('/stat'))
      return { data: { success: true, data: stat } };
    if (pathname.endsWith('/export')) return { data: 'csv-data' };
    return {
      data: {
        success: true,
        data: { items: [], page: 1, page_size: 20, total: 0 },
      },
    };
  });
  URL.createObjectURL = vi.fn(() => 'blob:export');
  URL.revokeObjectURL = vi.fn();
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
});
afterEach(cleanup);

describe('Classic usage log filter contracts', () => {
  it('renders content_parts with quota interpolation and preserves legacy fallback without mutating API rows', async () => {
    isAdmin.mockReturnValue(false);
    const { result } = renderHook(() => useLogsData());
    await waitFor(() => expect(result.current.loading).toBe(false));
    const rows = [
      {
        id: 1,
        type: 1,
        created_at: 1700000000,
        content: 'Email binding bonus $1',
        other: JSON.stringify({
          content_parts: [
            { key: 'Email binding bonus {{quota}}', params: { quota: '$1' } },
          ],
        }),
      },
      {
        id: 2,
        type: 1,
        created_at: 1700000000,
        content: 'legacy text',
        other: '{}',
      },
      {
        id: 3,
        type: 1,
        created_at: 1700000000,
        content: 'malformed parts fallback',
        other: JSON.stringify({ content_parts: [null] }),
      },
    ];
    act(() => result.current.setLogsFormat(rows));
    expect(result.current.logs.map((log) => log.content)).toEqual([
      '绑定邮箱赠送 $1',
      'legacy text',
      'malformed parts fallback',
    ]);
    expect(rows[0].content).toBe('Email binding bonus $1');
    await i18n.changeLanguage('en');
    act(() => result.current.setLogsFormat(rows));
    expect(result.current.logs[0].content).toBe('Email binding bonus $1');
  });

  it.each([false, true])(
    'only shows nested administrator reject reasons to admins (admin=%s)',
    async (admin) => {
      isAdmin.mockReturnValue(admin);
      const { result } = renderHook(() => useLogsData());
      await waitFor(() => expect(result.current.loading).toBe(false));
      act(() => {
        result.current.setLogsFormat([
          {
            id: 1,
            type: 2,
            created_at: 1700000000,
            model_name: 'gpt-4',
            other: JSON.stringify({
              reject_reason: 'legacy value must not leak',
              admin_info: { reject_reason: 'blocked by policy' },
            }),
          },
        ]);
      });
      const details = Object.values(result.current.expandData).flat();
      expect(details.filter((item) => item.key === '拦截原因')).toEqual(
        admin ? [{ key: '拦截原因', value: 'blocked by policy' }] : [],
      );
    },
  );

  it.each([false, true])(
    'preserves filters for list, statistics and export (admin=%s)',
    async (admin) => {
      isAdmin.mockReturnValue(admin);
      const { result } = renderHook(() => useLogsData());
      await waitFor(() => expect(result.current.loading).toBe(false));
      await act(async () => {
        result.current.setFormApi({ getValues: () => filters });
      });
      await waitFor(() => expect(result.current.stat).toEqual(stat));
      await act(async () => {
        await result.current.loadLogs(2, 50);
        await result.current.exportLogs();
      });

      const urls = API.get.mock.calls.map(
        ([url]) => new URL(url, 'http://localhost'),
      );
      const base = admin ? '/api/log' : '/api/log/self';
      for (const suffix of ['/', '/stat', '/export']) {
        const url = urls.findLast((item) => item.pathname === base + suffix);
        expect(url.hash).toBe('');
        for (const field of ['token_name', 'model_name', 'group']) {
          expect(url.searchParams.get(field)).toBe(special);
        }
        expect(url.searchParams.get('type')).toBe('2');
        expect(url.searchParams.get('start_timestamp')).toBe(
          String(Date.parse(filters.dateRange[0]) / 1000),
        );
        expect(url.searchParams.get('end_timestamp')).toBe(
          String(Date.parse(filters.dateRange[1]) / 1000),
        );
        if (suffix !== '/stat')
          expect(url.searchParams.get('request_id')).toBe(special);
        expect(url.searchParams.get('username')).toBe(admin ? special : null);
        expect(url.searchParams.get('channel')).toBe(admin ? '7' : null);
      }
      const list = urls.findLast((item) => item.pathname === base + '/');
      expect(list.searchParams.get('p')).toBe('2');
      expect(list.searchParams.get('page_size')).toBe('50');
      expect(showError).not.toHaveBeenCalled();
    },
  );

  it('unlocks statistics after a failed request so the next search can recover', async () => {
    isAdmin.mockReturnValue(false);
    const { result } = renderHook(() => useLogsData());
    await waitFor(() => expect(result.current.loading).toBe(false));
    API.get.mockRejectedValueOnce(new Error('temporary network failure'));
    await act(async () => {
      await result.current.handleEyeClick();
    });
    expect(result.current.loadingStat).toBe(false);
    expect(showError).toHaveBeenCalledOnce();
    await act(async () => {
      await result.current.handleEyeClick();
    });
    expect(result.current.stat).toEqual(stat);
  });
});
