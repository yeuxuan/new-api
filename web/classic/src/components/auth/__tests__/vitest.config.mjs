import { fileURLToPath } from 'node:url';

const web = fileURLToPath(new URL('../../../../../', import.meta.url));

export default {
  resolve: {
    alias: {
      react: `${web}node_modules/react`,
      'react/jsx-runtime': `${web}node_modules/react/jsx-runtime.js`,
      'react/jsx-dev-runtime': `${web}node_modules/react/jsx-dev-runtime.js`,
      'react-dom': `${web}node_modules/react-dom`,
      'react-dom/client': `${web}node_modules/react-dom/client.js`,
      'react-dom/test-utils': `${web}node_modules/react-dom/test-utils.js`,
      '@testing-library/react': `${web}node_modules/@testing-library/react`,
      '@lobehub/fluent-emoji/es/FluentEmoji': fileURLToPath(
        new URL(
          '../../../../node_modules/@lobehub/fluent-emoji/es/FluentEmoji/index.js',
          import.meta.url,
        ),
      ),
      '@lobehub/fluent-emoji': fileURLToPath(
        new URL('../../../../src/test-fluent-emoji.js', import.meta.url),
      ),
      '@lobehub/fluent-emoji/es/index.js': fileURLToPath(
        new URL('../../../../src/test-fluent-emoji.js', import.meta.url),
      ),
      '@lobehub/icons': fileURLToPath(
        new URL('../../../../src/test-lobe-icons.js', import.meta.url),
      ),
    },
    dedupe: ['react', 'react-dom'],
  },
  test: {
    environment: 'jsdom',
    include: [
      'src/components/auth/__tests__/*.test.jsx',
      'src/components/settings/personal/__tests__/*.test.jsx',
      'src/components/topup/__tests__/*.test.jsx',
      'src/hooks/usage-logs/__tests__/*.test.jsx',
    ],
    setupFiles: [
      fileURLToPath(new URL('../../../../src/test-setup.js', import.meta.url)),
    ],
    clearMocks: true,
    restoreMocks: true,
  },
};
