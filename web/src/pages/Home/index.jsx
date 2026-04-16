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

import React, { useContext, useEffect, useState, useMemo } from 'react';
import { Button, Typography } from '@douyinfe/semi-ui';
import { API, showError, copy, showSuccess } from '../../helpers';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { StatusContext } from '../../context/Status';
import { useActualTheme } from '../../context/Theme';
import { marked } from 'marked';
import { useTranslation } from 'react-i18next';
import {
  IconArrowRight,
  IconCopy,
  IconGithubLogo,
} from '@douyinfe/semi-icons';
import { Link } from 'react-router-dom';
import NoticeModal from '../../components/layout/NoticeModal';
import {
  Moonshot,
  OpenAI,
  XAI,
  Zhipu,
  Volcengine,
  Cohere,
  Claude,
  Gemini,
  Suno,
  Minimax,
  Wenxin,
  Spark,
  Qingyan,
  DeepSeek,
  Qwen,
  Midjourney,
  Grok,
  AzureAI,
  Hunyuan,
  Xinference,
} from '@lobehub/icons';

const { Text } = Typography;

// Provider icon data for the marquee
const PROVIDER_ICONS = [
  { Component: OpenAI, name: 'OpenAI' },
  { Component: Claude, name: 'Claude', color: true },
  { Component: Gemini, name: 'Gemini', color: true },
  { Component: DeepSeek, name: 'DeepSeek', color: true },
  { Component: Qwen, name: 'Qwen', color: true },
  { Component: Grok, name: 'Grok' },
  { Component: XAI, name: 'xAI' },
  { Component: Moonshot, name: 'Moonshot' },
  { Component: Zhipu, name: 'Zhipu', color: true },
  { Component: Volcengine, name: 'Volcengine', color: true },
  { Component: Cohere, name: 'Cohere', color: true },
  { Component: Minimax, name: 'Minimax', color: true },
  { Component: Wenxin, name: 'Wenxin', color: true },
  { Component: Spark, name: 'Spark', color: true },
  { Component: Qingyan, name: 'Qingyan', color: true },
  { Component: AzureAI, name: 'Azure', color: true },
  { Component: Hunyuan, name: 'Hunyuan', color: true },
  { Component: Xinference, name: 'Xinference', color: true },
  { Component: Suno, name: 'Suno' },
  { Component: Midjourney, name: 'Midjourney' },
];

const ProviderIcon = ({ item, size }) => {
  const IconComp = item.color ? item.Component.Color || item.Component : item.Component;
  return <IconComp size={size} />;
};

// Marquee row component
const MarqueeRow = ({ direction = 'left' }) => {
  const icons = useMemo(() => [...PROVIDER_ICONS, ...PROVIDER_ICONS], []);
  return (
    <div className='home-marquee-track' style={{ '--marquee-direction': direction === 'left' ? 'normal' : 'reverse' }}>
      {icons.map((item, i) => (
        <div
          key={`${item.name}-${i}`}
          className='home-marquee-item'
        >
          <ProviderIcon item={item} size={28} />
          <span className='text-xs text-semi-color-text-2 font-medium mt-1.5 whitespace-nowrap'>
            {item.name}
          </span>
        </div>
      ))}
    </div>
  );
};

// Terminal code block
const TerminalBlock = ({ serverAddress, t }) => {
  const codeLines = [
    { type: 'comment', text: `# ${t('只需替换 base_url')}` },
    { type: 'code', text: 'from openai import OpenAI' },
    { type: 'empty' },
    { type: 'code', text: 'client = OpenAI(' },
    { type: 'code', text: `    base_url="${serverAddress}/v1",` },
    { type: 'code', text: '    api_key="sk-..."' },
    { type: 'code', text: ')' },
    { type: 'empty' },
    { type: 'code', text: 'response = client.chat.completions.create(' },
    { type: 'code', text: '    model="gpt-4o",  ' },
    { type: 'comment-inline', text: `# ${t('或 claude, gemini, deepseek...')}` },
    { type: 'code', text: '    messages=[{"role": "user", "content": "Hi"}]' },
    { type: 'code', text: ')' },
  ];

  return (
    <div className='home-terminal'>
      <div className='home-terminal-bar'>
        <div className='home-terminal-dots'>
          <span className='home-terminal-dot home-terminal-dot-red' />
          <span className='home-terminal-dot home-terminal-dot-yellow' />
          <span className='home-terminal-dot home-terminal-dot-green' />
        </div>
        <span className='home-terminal-title'>quickstart.py</span>
        <div style={{ width: 52 }} />
      </div>
      <div className='home-terminal-body'>
        {codeLines.map((line, i) => {
          if (line.type === 'empty') return <div key={i} className='h-5' />;
          if (line.type === 'comment') {
            return (
              <div key={i} className='home-code-comment'>{line.text}</div>
            );
          }
          if (line.type === 'comment-inline') {
            return (
              <span key={i} className='home-code-comment'>{line.text}</span>
            );
          }
          return <div key={i} className='home-code-line'>{line.text}</div>;
        })}
      </div>
    </div>
  );
};

// Feature card component
const FeatureCard = ({ number, title, description }) => (
  <div className='home-feature-card'>
    <div className='home-feature-number'>{number}</div>
    <h3 className='home-feature-title'>{title}</h3>
    <p className='home-feature-desc'>{description}</p>
  </div>
);

const Home = () => {
  const { t, i18n } = useTranslation();
  const [statusState] = useContext(StatusContext);
  const actualTheme = useActualTheme();
  const [homePageContentLoaded, setHomePageContentLoaded] = useState(false);
  const [homePageContent, setHomePageContent] = useState('');
  const [noticeVisible, setNoticeVisible] = useState(false);
  const [copied, setCopied] = useState(false);
  const isMobile = useIsMobile();
  const isDemoSiteMode = statusState?.status?.demo_site_enabled || false;
  const serverAddress =
    statusState?.status?.server_address || `${window.location.origin}`;

  const displayHomePageContent = async () => {
    setHomePageContent(localStorage.getItem('home_page_content') || '');
    const res = await API.get('/api/home_page_content');
    const { success, message, data } = res.data;
    if (success) {
      let content = data;
      if (!data.startsWith('https://')) {
        content = marked.parse(data);
      }
      setHomePageContent(content);
      localStorage.setItem('home_page_content', content);

      if (data.startsWith('https://')) {
        const iframe = document.querySelector('iframe');
        if (iframe) {
          iframe.onload = () => {
            iframe.contentWindow.postMessage({ themeMode: actualTheme }, '*');
            iframe.contentWindow.postMessage({ lang: i18n.language }, '*');
          };
        }
      }
    } else {
      showError(message);
      setHomePageContent('加载首页内容失败...');
    }
    setHomePageContentLoaded(true);
  };

  const handleCopyBaseURL = async () => {
    const ok = await copy(serverAddress);
    if (ok) {
      showSuccess(t('已复制到剪切板'));
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  useEffect(() => {
    const checkNoticeAndShow = async () => {
      const lastCloseDate = localStorage.getItem('notice_close_date');
      const today = new Date().toDateString();
      if (lastCloseDate !== today) {
        try {
          const res = await API.get('/api/notice');
          const { success, data } = res.data;
          if (success && data && data.trim() !== '') {
            setNoticeVisible(true);
          }
        } catch (error) {
          console.error('获取公告失败:', error);
        }
      }
    };
    checkNoticeAndShow();
  }, []);

  useEffect(() => {
    displayHomePageContent().then();
  }, []);

  // Feature data
  const features = useMemo(
    () => [
      {
        number: '01',
        title: t('统一接口'),
        description: t(
          '一个 API 密钥，访问 40+ 大模型供应商。兼容 OpenAI API 格式，无需修改现有代码。',
        ),
      },
      {
        number: '02',
        title: t('智能路由'),
        description: t(
          '自动负载均衡与故障转移。请求智能分配到最优渠道，确保高可用性与低延迟。',
        ),
      },
      {
        number: '03',
        title: t('用量管控'),
        description: t(
          '精细的额度管理、速率限制与用量统计。按令牌计费，实时监控每一笔 API 调用。',
        ),
      },
    ],
    [t],
  );

  return (
    <div className='w-full overflow-x-hidden'>
      <NoticeModal
        visible={noticeVisible}
        onClose={() => setNoticeVisible(false)}
        isMobile={isMobile}
      />
      {homePageContentLoaded && homePageContent === '' ? (
        <div className='home-page'>
          {/* ===== HERO SECTION ===== */}
          <section className='home-hero'>
            <div className='home-hero-inner'>
              {/* Left: Text content */}
              <div className='home-hero-text'>
                <div className='home-hero-badge'>
                  {isDemoSiteMode && statusState?.status?.version && (
                    <span
                      className='home-version-tag'
                      onClick={() =>
                        window.open(
                          'https://github.com/QuantumNous/new-api',
                          '_blank',
                        )
                      }
                    >
                      <IconGithubLogo size='small' />
                      <span>{statusState.status.version}</span>
                    </span>
                  )}
                </div>

                <h1 className='home-headline'>
                  {t('统一的')}{' '}
                  <span className='home-headline-accent'>
                    {t('大模型接口网关')}
                  </span>
                </h1>

                <p className='home-subline'>
                  {t(
                    '聚合 40+ AI 供应商，兼容 OpenAI 格式。更好的价格，更高的稳定性。',
                  )}
                </p>

                {/* Base URL copy block */}
                <div className='home-url-block' onClick={handleCopyBaseURL}>
                  <code className='home-url-text'>{serverAddress}</code>
                  <button className='home-url-copy'>
                    <IconCopy size='small' />
                    <span>{copied ? t('已复制') : t('复制')}</span>
                  </button>
                </div>

                {/* CTA buttons */}
                <div className='home-cta-group'>
                  <Link to='/console'>
                    <Button
                      theme='solid'
                      type='primary'
                      size={isMobile ? 'default' : 'large'}
                      className='home-cta-primary'
                      iconPosition='right'
                      icon={<IconArrowRight />}
                    >
                      {t('开始使用')}
                    </Button>
                  </Link>
                  <Link to='/pricing'>
                    <Button
                      type='tertiary'
                      size={isMobile ? 'default' : 'large'}
                      className='home-cta-secondary'
                    >
                      {t('查看模型与价格')}
                    </Button>
                  </Link>
                </div>
              </div>

              {/* Right: Terminal */}
              {!isMobile && (
                <div className='home-hero-visual'>
                  <TerminalBlock serverAddress={serverAddress} t={t} />
                </div>
              )}
            </div>
          </section>

          {/* ===== PROVIDER MARQUEE ===== */}
          <section className='home-providers'>
            <div className='home-providers-label'>
              <Text className='text-sm font-semibold tracking-widest uppercase text-semi-color-text-2'>
                {t('支持众多的大模型供应商')}
              </Text>
            </div>
            <div className='home-marquee'>
              <div className='home-marquee-fade home-marquee-fade-left' />
              <div className='home-marquee-fade home-marquee-fade-right' />
              <MarqueeRow direction='left' />
            </div>
          </section>

          {/* ===== FEATURES ===== */}
          <section className='home-features'>
            <div className='home-features-inner'>
              {features.map((f) => (
                <FeatureCard key={f.number} {...f} />
              ))}
            </div>
          </section>

          {/* ===== MOBILE TERMINAL ===== */}
          {isMobile && (
            <section className='px-4 pb-12'>
              <TerminalBlock serverAddress={serverAddress} t={t} />
            </section>
          )}

          {/* ===== BOTTOM CTA ===== */}
          <section className='home-bottom-cta'>
            <h2 className='home-bottom-cta-title'>
              {t('准备好了吗？')}
            </h2>
            <p className='home-bottom-cta-desc'>
              {t('注册即可获得免费额度，几分钟内开始调用 API。')}
            </p>
            <Link to='/console'>
              <Button
                theme='solid'
                type='primary'
                size='large'
                className='home-cta-primary'
                iconPosition='right'
                icon={<IconArrowRight />}
              >
                {t('免费开始')}
              </Button>
            </Link>
          </section>
        </div>
      ) : (
        <div className='overflow-x-hidden w-full'>
          {homePageContent.startsWith('https://') ? (
            <iframe
              src={homePageContent}
              className='w-full h-screen border-none'
            />
          ) : (
            <div
              className='mt-[60px]'
              dangerouslySetInnerHTML={{ __html: homePageContent }}
            />
          )}
        </div>
      )}
    </div>
  );
};

export default Home;
