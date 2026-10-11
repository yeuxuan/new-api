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

import React, { useState, useEffect } from 'react';
import { Modal, Button, Typography, Spin } from '@douyinfe/semi-ui';
import { IconExternalOpen, IconCopy } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { Download } from 'lucide-react';

const { Text } = Typography;

const TaskArtifactPreview = (props) => {
  const { t } = useTranslation();
  const [mediaFailed, setMediaFailed] = useState(false);
  const artifact = props.artifact;
  let media = null;
  if (mediaFailed) {
    media = <Text type='warning'>{t('Preview unavailable')}</Text>;
  } else if (artifact.type === 'image') {
    media = (
      <img
        src={artifact.content_url}
        alt={artifact.key}
        loading='lazy'
        referrerPolicy='no-referrer'
        onError={() => setMediaFailed(true)}
        style={{ width: '100%', maxHeight: '50vh', objectFit: 'contain' }}
      />
    );
  } else if (artifact.type === 'video') {
    media = (
      <video
        src={artifact.content_url}
        aria-label={artifact.key}
        controls
        preload='none'
        onError={() => setMediaFailed(true)}
        style={{ width: '100%', maxHeight: '50vh' }}
      />
    );
  } else if (artifact.type === 'audio') {
    media = (
      <audio
        src={artifact.content_url}
        aria-label={artifact.key}
        controls
        preload='none'
        onError={() => setMediaFailed(true)}
        style={{ width: '100%' }}
      />
    );
  }

  const downloadArtifact = () => {
    const link = document.createElement('a');
    link.href = artifact.content_url;
    link.download = artifact.key;
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    link.click();
  };

  return (
    <section
      aria-label={artifact.key}
      style={{
        minWidth: 0,
        borderBottom: '1px solid var(--semi-color-border)',
        paddingBottom: 16,
      }}
    >
      <Text
        strong
        style={{ display: 'block', overflowWrap: 'anywhere', marginBottom: 8 }}
      >
        {artifact.key}
      </Text>
      {media}
      <Button
        icon={<Download size={16} aria-hidden='true' />}
        onClick={downloadArtifact}
        style={{ marginTop: 8 }}
      >
        {t('Download')}
      </Button>
    </section>
  );
};

const ContentModal = ({
  isModalOpen,
  setIsModalOpen,
  modalContent,
  isVideo,
  artifacts,
}) => {
  const { t } = useTranslation();
  const [videoError, setVideoError] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const isArtifactPreview = Array.isArray(artifacts);

  useEffect(() => {
    if (isModalOpen && isVideo) {
      setVideoError(false);
      setIsLoading(true);
    }
  }, [isModalOpen, isVideo, modalContent]);

  const handleVideoError = () => {
    setVideoError(true);
    setIsLoading(false);
  };

  const handleVideoLoaded = () => {
    setIsLoading(false);
  };

  const handleCopyUrl = () => {
    navigator.clipboard.writeText(modalContent);
  };

  const handleOpenInNewTab = () => {
    window.open(modalContent, '_blank');
  };

  const renderVideoContent = () => {
    if (videoError) {
      return (
        <div style={{ textAlign: 'center', padding: '40px' }}>
          <Text
            type='tertiary'
            style={{ display: 'block', marginBottom: '16px' }}
          >
            {t('视频无法在当前浏览器中播放，这可能是由于：')}
          </Text>
          <Text
            type='tertiary'
            style={{ display: 'block', marginBottom: '8px', fontSize: '12px' }}
          >
            {t('• 视频服务商的跨域限制')}
          </Text>
          <Text
            type='tertiary'
            style={{ display: 'block', marginBottom: '8px', fontSize: '12px' }}
          >
            {t('• 需要特定的请求头或认证')}
          </Text>
          <Text
            type='tertiary'
            style={{ display: 'block', marginBottom: '16px', fontSize: '12px' }}
          >
            {t('• 防盗链保护机制')}
          </Text>

          <div style={{ marginTop: '20px' }}>
            <Button
              icon={<IconExternalOpen />}
              onClick={handleOpenInNewTab}
              style={{ marginRight: '8px' }}
            >
              {t('在新标签页中打开')}
            </Button>
            <Button icon={<IconCopy />} onClick={handleCopyUrl}>
              {t('复制链接')}
            </Button>
          </div>

          <div
            style={{
              marginTop: '16px',
              padding: '8px',
              backgroundColor: '#f8f9fa',
              borderRadius: '4px',
            }}
          >
            <Text
              type='tertiary'
              style={{ fontSize: '10px', wordBreak: 'break-all' }}
            >
              {modalContent}
            </Text>
          </div>
        </div>
      );
    }

    return (
      <div style={{ position: 'relative', height: '100%' }}>
        {isLoading && (
          <div
            style={{
              position: 'absolute',
              top: '50%',
              left: '50%',
              transform: 'translate(-50%, -50%)',
              zIndex: 10,
            }}
          >
            <Spin size='large' />
          </div>
        )}
        <video
          src={modalContent}
          controls
          style={{
            width: '100%',
            height: '100%',
            maxWidth: '100%',
            maxHeight: '100%',
            objectFit: 'contain',
          }}
          onError={handleVideoError}
          onLoadedData={handleVideoLoaded}
          onLoadStart={() => setIsLoading(true)}
        />
      </div>
    );
  };

  let content = <p style={{ whiteSpace: 'pre-line' }}>{modalContent}</p>;
  if (isVideo) {
    content = renderVideoContent();
  } else if (isArtifactPreview) {
    content = (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        {artifacts.map((artifact) => (
          <TaskArtifactPreview
            key={`${artifact.key}:${artifact.content_url}`}
            artifact={artifact}
          />
        ))}
      </div>
    );
  }

  return (
    <Modal
      title={isArtifactPreview ? t('Artifacts') : undefined}
      visible={isModalOpen}
      onOk={() => setIsModalOpen(false)}
      onCancel={() => setIsModalOpen(false)}
      closable={null}
      bodyStyle={{
        height: isVideo || isArtifactPreview ? '70vh' : '400px',
        maxHeight: '80vh',
        overflow: 'auto',
        padding: isVideo && videoError ? '0' : '24px',
      }}
      width={isVideo || isArtifactPreview ? '90vw' : 800}
      style={isVideo || isArtifactPreview ? { maxWidth: 960 } : undefined}
    >
      {content}
    </Modal>
  );
};

export default ContentModal;
