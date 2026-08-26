import React from 'react';
import { BrowserRouter } from 'react-router-dom';
import ReactDOM from 'react-dom/client';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';
import './style/index.css';

import theme from './theme';
import App from './App';
import { initGTM } from './utils/gtm';
import { appConfig } from '~/utils/config';

dayjs.locale('zh-cn');

/** 开发环境屏蔽 antd 弃用 API 控制台警告（不影响功能，升级 antd 6 前再改对应 props） */
if (import.meta.env.DEV) {
  const nativeWarn = console.warn.bind(console);
  console.warn = (...args: unknown[]) => {
    const first = args[0];
    if (typeof first === 'string' && first.includes('[antd:')) return;
    nativeWarn(...args);
  };
}

if (appConfig.enableGtm && appConfig.gtmId) {
  initGTM(appConfig.gtmId);
}

const root = ReactDOM.createRoot(document.getElementById('root') as HTMLElement);
root.render(
  <React.StrictMode>
    <BrowserRouter
      future={{
        v7_startTransition: true,
        v7_relativeSplatPath: true,
      }}
    >
      <ConfigProvider locale={zhCN} theme={theme}>
        <App />
      </ConfigProvider>
    </BrowserRouter>
  </React.StrictMode>
);
