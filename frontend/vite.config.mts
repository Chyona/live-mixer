/// <reference types="vitest/config" />
import { defineConfig, loadEnv } from 'vite';
import { createHtmlPlugin } from 'vite-plugin-html';
import svg from 'vite-plugin-svgr';
import tsconfigPaths from 'vite-tsconfig-paths';
import react from '@vitejs/plugin-react-swc';
import { viteMockServe } from 'vite-plugin-mock';

/**
 * 生产环境 CSP：收紧 script 来源以降低 XSS 窃取 localStorage Token 的风险。
 * Ant Design 依赖内联 style；媒体/图片允许 https（直播源与 TOS）。
 * 开发环境不注入，避免打断 Vite HMR。
 */
const PRODUCTION_CSP = [
  "default-src 'self'",
  "base-uri 'self'",
  "object-src 'none'",
  "form-action 'self'",
  "script-src 'self' https://www.googletagmanager.com https://www.google-analytics.com",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: blob: https: http:",
  "media-src 'self' blob: https: http:",
  "font-src 'self' data:",
  "connect-src 'self' https: wss: ws:",
  "worker-src 'self' blob:",
  "frame-src 'self' https://www.googletagmanager.com https://mp.weixin.qq.com",
].join('; ');

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');

  const apiPrefix = env.VITE_API_PREFIX || '/openapi';
  const useMock = env.VITE_MOCK === 'true' || process.env.VITE_MOCK === 'true';
  const appTitle = env.VITE_APP_TITLE || 'Base UI';
  const appDescription =
    env.VITE_APP_DESCRIPTION || '基于 React + Vite + Ant Design 的前端项目模板';
  const isProduction = mode === 'production';

  return {
    clearScreen: false,
    server: {
      host: true,
      port: 8008,
      open: true,
      proxy: {
        [apiPrefix]: {
          target: env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:3000',
          changeOrigin: true,
        },
        '/tos-media': {
          target: 'https://arkclaw-wxbpd.tos-cn-shanghai.volces.com',
          changeOrigin: true,
          secure: true,
          rewrite: (path) => path.replace(/^\/tos-media/, ''),
        },
      },
    },
    build: {
      chunkSizeWarningLimit: 1500,
      rollupOptions: {
        treeshake: 'recommended',
        output: {
          manualChunks: {
            react: ['react', 'react-dom', 'react-router-dom'],
            antd: ['antd', '@ant-design/icons'],
            hls: ['hls.js'],
          },
        },
      },
      // 默认不产出 sourcemap；需要时设置 GENERATE_SOURCEMAP=true
      sourcemap: process.env.GENERATE_SOURCEMAP === 'true',
      outDir: 'dist',
      assetsDir: '.',
    },
    plugins: [
      viteMockServe({
        mockPath: 'mock',
        enable: mode === 'development' && useMock,
        watchFiles: true,
      }),
      tsconfigPaths(),
      react(),
      svg({ svgrOptions: { icon: true } }),
      createHtmlPlugin({
        minify: true,
        inject: {
          data: {
            title: appTitle,
            description: appDescription,
            cspMeta: isProduction
              ? `<meta http-equiv="Content-Security-Policy" content="${PRODUCTION_CSP}" />`
              : '',
          },
        },
      }),
    ],
    css: {
      modules: {
        localsConvention: 'camelCaseOnly',
      },
    },
    test: {
      environment: 'jsdom',
      setupFiles: './src/setupTests.ts',
      include: ['src/**/*.{test,spec}.{ts,tsx}'],
    },
  };
});
